package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/shared/constant"
)

const systemPrompt = `You help a home buyer rank homes for sale by how well they match the buyer's taste.

You judge ONLY from the photos: architecture, exterior materials (panels, brick, render, wood),
facade, roof, windows, garden, interior style, materials, kitchen, bathrooms, light, layout and character.
You do not get the ad text and must not guess about things the photos do not show.

You get the buyer's description of what they want, photos of reference homes the buyer likes,
and photos of one candidate home. Score how well the candidate matches the buyer's taste from 0 to 100:
- 90-100: an excellent match, very close to the description and references
- 70-89: a good match with some differences
- 40-69: partly matching
- 0-39: a poor match

Weigh the buyer's description most where it states what they care about most.
Write "style" as a short label of the candidate's style (max 6 words) and "summary" as 1-2 sentences
explaining the score, in the same language as the buyer's description.`

var scoreSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"score":   map[string]any{"type": "integer", "description": "Match score from 0 to 100"},
		"style":   map[string]any{"type": "string"},
		"summary": map[string]any{"type": "string"},
	},
	"required":             []string{"score", "style", "summary"},
	"additionalProperties": false,
}

type scoreResult struct {
	Score   int64  `json:"score"`
	Style   string `json:"style"`
	Summary string `json:"summary"`
}

type ScoringStatus struct {
	Running bool   `json:"running"`
	Version int64  `json:"version"`
	Scored  int    `json:"scored"`
	Total   int    `json:"total"`
	Pending bool   `json:"pending"` // a run is scheduled (debounced)
	Paused  bool   `json:"paused"`
	Error   string `json:"error,omitempty"`
}

type Scorer struct {
	db          *DB
	images      *ImageCache
	client      anthropic.Client
	model       string
	effort      anthropic.BetaOutputConfigEffort
	concurrency int
	maxImages   int
	maxRefImgs  int

	mu       sync.Mutex
	cancel   context.CancelFunc
	running  bool
	version  int64
	timer    *time.Timer
	dirty    bool
	lastErr  string
	debounce time.Duration
}

// Trigger schedules a scoring run after a short quiet period, so that several quick changes
// (adding references, editing the prompt, ingest batches) only cause one run.
func (s *Scorer) Trigger() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(s.debounce, s.start)
}

// Pause stops the running job (in-flight requests are cancelled) and blocks new runs until Resume.
func (s *Scorer) Pause() error {
	if err := s.db.setPaused(true); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.running {
		s.cancel()
		s.running = false
		s.version = 0
	}
	return nil
}

func (s *Scorer) Resume() error {
	if err := s.db.setPaused(false); err != nil {
		return err
	}
	s.Trigger()
	return nil
}

func (s *Scorer) start() {
	if s.db.paused() {
		s.mu.Lock()
		s.timer = nil
		s.mu.Unlock()
		return
	}
	settings, err := s.db.settings()
	if err != nil {
		log.Printf("scorer: read settings: %v", err)
		return
	}
	s.mu.Lock()
	s.timer = nil
	if s.running {
		if s.version == settings.Version {
			// Same version: let the current run finish, then pick up new listings.
			s.dirty = true
			s.mu.Unlock()
			return
		}
		// Prompt or references changed: scores from the running job are now stale.
		s.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.running = true
	s.version = settings.Version
	s.lastErr = ""
	s.mu.Unlock()

	go func() {
		for {
			err := s.run(ctx, settings)
			s.mu.Lock()
			if err != nil && !errors.Is(err, context.Canceled) {
				s.lastErr = err.Error()
				log.Printf("scorer: %v", err)
			}
			if s.version != settings.Version || ctx.Err() != nil {
				// A newer run has taken over.
				s.mu.Unlock()
				return
			}
			again := s.dirty
			s.dirty = false
			if !again {
				s.running = false
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
		}
	}()
}

func (s *Scorer) Status() ScoringStatus {
	settings, _ := s.db.settings()
	scored, total, _ := s.db.scoreCounts(settings.Version)
	s.mu.Lock()
	defer s.mu.Unlock()
	return ScoringStatus{
		Running: s.running,
		Version: settings.Version,
		Scored:  scored,
		Total:   total,
		Pending: s.timer != nil,
		Paused:  s.db.paused(),
		Error:   s.lastErr,
	}
}

func (s *Scorer) run(ctx context.Context, settings Settings) error {
	todo, err := s.db.unscored(settings.Version)
	if err != nil {
		return err
	}
	if len(todo) == 0 {
		return nil
	}
	prefix, err := s.buildPrefix(ctx, settings.Prompt)
	if err != nil {
		return err
	}
	log.Printf("scorer: scoring %d listings for version %d", len(todo), settings.Version)

	jobs := make(chan Listing)
	var wg sync.WaitGroup
	for i := 0; i < s.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for l := range jobs {
				s.scoreOne(ctx, settings.Version, s.modelFor(settings), prefix, l)
			}
		}()
	}
	for _, l := range todo {
		select {
		case jobs <- l:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
	return ctx.Err()
}

// buildPrefix builds the content shared by every request in a run: the buyer's description and
// the reference photos. It ends with a cache breakpoint so it is only paid for in full once.
func (s *Scorer) buildPrefix(ctx context.Context, prompt string) ([]anthropic.BetaContentBlockParamUnion, error) {
	blocks := []anthropic.BetaContentBlockParamUnion{
		anthropic.NewBetaTextBlock("## What I am looking for\n\n" + prompt),
	}
	refs, err := s.db.references()
	if err != nil {
		return nil, err
	}
	n := 0
	for _, ref := range refs {
		if len(ref.Images) == 0 {
			continue
		}
		n++
		blocks = append(blocks, anthropic.NewBetaTextBlock(fmt.Sprintf("## Reference home %d that I like", n)))
		for _, url := range pickEvenly(ref.Images, s.maxRefImgs) {
			b, err := s.imageBlock(ctx, url)
			if err != nil {
				log.Printf("scorer: reference image: %v", err)
				continue
			}
			blocks = append(blocks, b)
		}
	}
	if n == 0 {
		blocks = append(blocks, anthropic.NewBetaTextBlock("(No reference homes have been added yet.)"))
	}
	last := blocks[len(blocks)-1]
	if last.OfImage != nil {
		last.OfImage.CacheControl = anthropic.NewBetaCacheControlEphemeralParam()
	} else if last.OfText != nil {
		last.OfText.CacheControl = anthropic.NewBetaCacheControlEphemeralParam()
	}
	return blocks, nil
}

func (s *Scorer) imageBlock(ctx context.Context, url string) (anthropic.BetaContentBlockParamUnion, error) {
	data, err := s.images.get(ctx, url)
	if err != nil {
		return anthropic.BetaContentBlockParamUnion{}, err
	}
	return anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
		Data:      base64.StdEncoding.EncodeToString(data),
		MediaType: anthropic.BetaBase64ImageSourceMediaTypeImageJPEG,
	}), nil
}

type ModelOption struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Note    string `json:"note"`
	Default bool   `json:"default"`
}

// models the user can pick from. Costs are rough per-home estimates with 10 photos.
var models = []ModelOption{
	{ID: "claude-opus-5-5", Label: "Claude Opus 5.5", Note: "Strong visual judgment, about $0.06 per home"},
	{ID: "claude-sonnet-5-5", Label: "Claude Sonnet 5.5", Note: "Good balance, about $0.03 per home"},
	{ID: "claude-haiku-5-5", Label: "Claude Haiku 5.5", Note: "Fast and very cheap, about $0.002 per home"},
	{ID: "claude-fable-5-1", Label: "Claude Fable 5.1", Note: "Most capable, about $0.15 per home"},
}

func validModel(id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}
	return false
}

func (s *Scorer) modelFor(settings Settings) string {
	if settings.Model != "" {
		return settings.Model
	}
	return s.model
}

// Server-side refusal fallbacks are only offered for some models.
func supportsFallbacks(model string) bool {
	switch model {
	case "claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-1":
		return true
	}
	return false
}

func (s *Scorer) scoreOne(ctx context.Context, version int64, model string, prefix []anthropic.BetaContentBlockParamUnion, l Listing) {
	if ctx.Err() != nil {
		return
	}
	content := append([]anthropic.BetaContentBlockParamUnion{}, prefix...)
	content = append(content, anthropic.NewBetaTextBlock("## Candidate home"))
	added := 0
	for _, url := range pickEvenly(l.Images, s.maxImages) {
		b, err := s.imageBlock(ctx, url)
		if err != nil {
			log.Printf("scorer: %d: %v", l.Finnkode, err)
			continue
		}
		content = append(content, b)
		added++
	}
	if added == 0 {
		msg := "could not download any images"
		_ = s.db.saveScore(l.Finnkode, version, nil, nil, nil, &msg)
		return
	}
	content = append(content, anthropic.NewBetaTextBlock("Score how well the candidate home matches my taste."))

	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: systemPrompt}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(content...)},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: s.effort,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: scoreSchema},
		},
	}
	if supportsFallbacks(model) {
		// If a request is declined by a safety classifier, let the API retry it on a fallback model.
		params.Fallbacks = anthropic.BetaFallbacksParamUnion{OfDefault: constant.ValueOf[constant.Default]()}
		params.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
	}
	resp, err := s.client.Beta.Messages.New(ctx, params)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		var apiErr *anthropic.Error
		msg := err.Error()
		if errors.As(err, &apiErr) {
			msg = fmt.Sprintf("API error %d", apiErr.StatusCode)
		}
		log.Printf("scorer: %d: %v", l.Finnkode, err)
		s.setErr(msg)
		// Transient errors (rate limits, overload) are not stored, so the listing is retried next run.
		if apiErr != nil && (apiErr.StatusCode == 429 || apiErr.StatusCode >= 500) {
			return
		}
		_ = s.db.saveScore(l.Finnkode, version, nil, nil, nil, &msg)
		return
	}
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		msg := "Claude declined to score this listing"
		_ = s.db.saveScore(l.Finnkode, version, nil, nil, nil, &msg)
		return
	}
	var text string
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text += t.Text
		}
	}
	var res scoreResult
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		msg := fmt.Sprintf("could not parse response (stop reason %s)", resp.StopReason)
		_ = s.db.saveScore(l.Finnkode, version, nil, nil, nil, &msg)
		return
	}
	score := max(0, min(100, res.Score))
	if err := s.db.saveScore(l.Finnkode, version, &score, &res.Style, &res.Summary, nil); err != nil {
		log.Printf("scorer: save %d: %v", l.Finnkode, err)
	}
	log.Printf("scorer: %d scored %d (cache read %d, input %d, output %d)", l.Finnkode, score,
		resp.Usage.CacheReadInputTokens, resp.Usage.InputTokens, resp.Usage.OutputTokens)
}

func (s *Scorer) setErr(msg string) {
	s.mu.Lock()
	s.lastErr = msg
	s.mu.Unlock()
}
