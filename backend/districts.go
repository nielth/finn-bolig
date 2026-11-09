package main

import "math"

type District struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// Approximate centre points of the Stavanger bydeler. A listing is assigned to the
// nearest centre, which is good enough for filtering but can be off near borders.
var districts = []District{
	{"Sentrum", 58.9700, 5.7330},
	{"Storhaug", 58.9640, 5.7530},
	{"Eiganes og Våland", 58.9620, 5.7200},
	{"Hundvåg", 58.9900, 5.7350},
	{"Tasta", 58.9830, 5.6990},
	{"Madla", 58.9480, 5.6600},
	{"Hillevåg", 58.9450, 5.7350},
	{"Hinna", 58.9150, 5.7350},
	{"Rennesøy", 59.0950, 5.7000},
	{"Finnøy", 59.1700, 5.8600},
}

// districtFor returns the nearest district, or "" if the point is far outside Stavanger.
func districtFor(lat, lon float64) string {
	best := ""
	bestDist := math.MaxFloat64
	for _, d := range districts {
		dist := haversineKm(lat, lon, d.Lat, d.Lon)
		if dist < bestDist {
			best, bestDist = d.Name, dist
		}
	}
	if bestDist > 12 {
		return ""
	}
	return best
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}
