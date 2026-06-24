// internal/fetcher/fetcher.go
// Fetches and caches data from:
//   - jsDelivr CDN (Him97kr/rest-countries-data)  (population, density, region, etc.)
//   - disease.sh API         (COVID-19 stats)
//   - WHO Outbreak News API  (disease alerts)

package fetcher

import (
	"encoding/json"
	"fmt"
	"math"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	restCountriesURL = "https://cdn.jsdelivr.net/gh/Him97kr/rest-countries-data/allcountries.json"
	covidURL         = "https://disease.sh/v3/covid-19/countries?allowNull=true"
	whoURL           = "https://www.who.int/api/news/diseaseoutbreaknews?sf_culture=en&$top=100&$orderby=PublicationDateAndTime%20desc"
	cacheTTL         = 30 * time.Minute
)

// ── Raw API structs ───────────────────────────────────────────────────────────

// cdnResponse is the root wrapper from the jsDelivr CDN JSON.
type cdnResponse struct {
	CountryData []restCountry `json:"countryData"`
}

type restCountry struct {
	Names struct {
		Common   string `json:"common"`
		Official string `json:"official"`
	} `json:"names"`
	Codes struct {
		Alpha2 string `json:"alpha_2"`
		Alpha3 string `json:"alpha_3"`
	} `json:"codes"`
	Capitals []struct {
		Name string `json:"name"`
	} `json:"capitals"`
	Region     string `json:"region"`
	Population int    `json:"population"`
	Area       struct {
		Kilometers float64 `json:"kilometers"`
		Miles      float64 `json:"miles"`
	} `json:"area"`
	Flag struct {
		Emoji  string `json:"emoji"`
		UrlPNG string `json:"url_png"`
		UrlSVG string `json:"url_svg"`
	} `json:"flag"`
	Languages []struct {
		Name string `json:"name"`
	} `json:"languages"`
	Currencies []struct {
		Code   string `json:"code"`
		Name   string `json:"name"`
		Symbol string `json:"symbol"`
	} `json:"currencies"`
}

type covidCountry struct {
	Country     string `json:"country"`
	CountryInfo struct {
		ISO3 string `json:"iso3"` // e.g. "USA", "GBR", "PRK"
	} `json:"countryInfo"`
	Cases            int     `json:"cases"`
	TodayCases       int     `json:"todayCases"`
	Deaths           int     `json:"deaths"`
	TodayDeaths      int     `json:"todayDeaths"`
	Recovered        int     `json:"recovered"`
	Active           int     `json:"active"`
	Critical         int     `json:"critical"`
	CasesPerMillion  float64 `json:"casesPerOneMillion"`
	DeathsPerMillion float64 `json:"deathsPerOneMillion"`
	Tests            int     `json:"tests"`
	Updated          int64   `json:"updated"`
}

type whoResponse struct {
	Value []struct {
		Title                  string `json:"Title"`
		PublicationDateAndTime string `json:"PublicationDateAndTime"`
		UrlName                string `json:"UrlName"`
		Summary                string `json:"Summary"`
	} `json:"value"`
}

// ── Normalised models ─────────────────────────────────────────────────────────

type Country struct {
	Name       string
	Code       string
	Capital    string
	Region     string
	Population int
	Density    float64
	Area       float64
	Flag       string
	Languages  []string
	Currencies []string
}

type CovidStats struct {
	Country          string
	ISO3Code         string  // ISO alpha3 from disease.sh countryInfo.iso3
	Cases            int
	TodayCases       int
	Deaths           int
	TodayDeaths      int
	Recovered        int
	Active           int
	Critical         int
	CasesPerMillion  float64
	DeathsPerMillion float64
	Tests            int
	UpdatedAt        string
}

type Outbreak struct {
	Title   string
	Date    string
	UrlName string
	Summary string
}

// ── Generic cache ─────────────────────────────────────────────────────────────

type cache[T any] struct {
	mu        sync.RWMutex
	data      T
	fetchedAt time.Time
	ready     bool
}

func (c *cache[T]) get() (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.ready || time.Since(c.fetchedAt) > cacheTTL {
		var zero T
		return zero, false
	}
	return c.data, true
}

func (c *cache[T]) set(data T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
	c.fetchedAt = time.Now()
	c.ready = true
}

// ── Fetcher ───────────────────────────────────────────────────────────────────

type Fetcher struct {
	client         *http.Client
	countriesCache cache[[]Country]
	covidCache     cache[[]CovidStats]
	outbreakCache  cache[[]Outbreak]
}

func New() *Fetcher {
	return &Fetcher{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (f *Fetcher) fetchJSON(url string, target any) error {
	resp, err := f.client.Get(url)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("unmarshal: %w — body prefix: %.120s", err, string(body))
	}
	return nil
}

// ── Countries ─────────────────────────────────────────────────────────────────

func (f *Fetcher) Countries() ([]Country, error) {
	if data, ok := f.countriesCache.get(); ok {
		return data, nil
	}

	var cdnData cdnResponse
	if err := f.fetchJSON(restCountriesURL, &cdnData); err != nil {
		return nil, err
	}
	raw := cdnData.CountryData

	countries := make([]Country, 0, len(raw))
	for _, r := range raw {
		// capitals is [{name}] in CDN
		capital := ""
		if len(r.Capitals) > 0 {
			capital = r.Capitals[0].Name
		}

		langs := make([]string, 0, len(r.Languages))
		for _, v := range r.Languages {
			langs = append(langs, v.Name)
		}

		currs := make([]string, 0, len(r.Currencies))
		for _, v := range r.Currencies {
			currs = append(currs, v.Name)
		}

		// density is not provided by CDN — compute from population / area.kilometers
		var density float64
		if r.Area.Kilometers > 0 {
			density = math.Round(float64(r.Population) / r.Area.Kilometers*100) / 100 // round to 2 decimals
		}

		countries = append(countries, Country{
			Name:       r.Names.Common,
			Code:       r.Codes.Alpha3,
			Capital:    capital,
			Region:     r.Region,
			Population: r.Population,
			Density:    density,
			Area:       r.Area.Kilometers,
			Flag:       r.Flag.UrlSVG,
			Languages:  langs,
			Currencies: currs,
		})
	}

	f.countriesCache.set(countries)
	return countries, nil
}

func (f *Fetcher) FindCountry(name, code string) (*Country, error) {
	countries, err := f.Countries()
	if err != nil {
		return nil, err
	}
	q  := strings.ToLower(strings.TrimSpace(name))
	qc := strings.ToUpper(strings.TrimSpace(code))

	// Pass 1 — exact ISO code match
	if qc != "" {
		for i, c := range countries {
			if strings.EqualFold(c.Code, qc) {
				return &countries[i], nil
			}
		}
	}

	if q == "" {
		return nil, nil
	}

	// Pass 2 — exact name match
	for i, c := range countries {
		if strings.ToLower(c.Name) == q {
			return &countries[i], nil
		}
	}

	// Pass 3 — country name starts with query
	// "india" matches "India" before "British Indian Ocean Territory"
	for i, c := range countries {
		if strings.HasPrefix(strings.ToLower(c.Name), q) {
			return &countries[i], nil
		}
	}

	// Pass 4 — query covers >50% of country name length
	for i, c := range countries {
		cname := strings.ToLower(c.Name)
		if len(q) >= 4 && strings.Contains(cname, q) &&
			float64(len(q))/float64(len(cname)) > 0.5 {
			return &countries[i], nil
		}
	}

	// Pass 5 — country name contained within query string
	for i, c := range countries {
		cname := strings.ToLower(c.Name)
		if len(cname) >= 4 && strings.Contains(q, cname) {
			return &countries[i], nil
		}
	}
	return nil, nil
}

// ── COVID stats ───────────────────────────────────────────────────────────────

func (f *Fetcher) CovidStats() ([]CovidStats, error) {
	if data, ok := f.covidCache.get(); ok {
		return data, nil
	}

	var raw []covidCountry
	if err := f.fetchJSON(covidURL, &raw); err != nil {
		return nil, err
	}

	stats := make([]CovidStats, 0, len(raw))
	for _, r := range raw {
		updatedAt := ""
		if r.Updated > 0 {
			updatedAt = time.UnixMilli(r.Updated).UTC().Format(time.RFC3339)
		}
		stats = append(stats, CovidStats{
			Country:          r.Country,
			ISO3Code:         r.CountryInfo.ISO3,
			Cases:            r.Cases,
			TodayCases:       r.TodayCases,
			Deaths:           r.Deaths,
			TodayDeaths:      r.TodayDeaths,
			Recovered:        r.Recovered,
			Active:           r.Active,
			Critical:         r.Critical,
			CasesPerMillion:  r.CasesPerMillion,
			DeathsPerMillion: r.DeathsPerMillion,
			Tests:            r.Tests,
			UpdatedAt:        updatedAt,
		})
	}

	f.covidCache.set(stats)
	return stats, nil
}

func (f *Fetcher) FindCovid(countryName string) (*CovidStats, error) {
	return f.FindCovidByCode("", countryName)
}

// FindCovidByCode looks up COVID stats by ISO3 code first, then falls back to name matching.
func (f *Fetcher) FindCovidByCode(iso3, countryName string) (*CovidStats, error) {
	stats, err := f.CovidStats()
	if err != nil {
		return nil, err
	}

	// Pass 1 — exact ISO3 code match (most reliable — avoids all name mismatch issues)
	// Covers: USA, GBR, UAE, PRK, KOR, etc.
	if iso3 != "" {
		code := strings.ToUpper(strings.TrimSpace(iso3))
		for i, s := range stats {
			if strings.EqualFold(s.ISO3Code, code) {
				return &stats[i], nil
			}
		}
	}

	if countryName == "" {
		return nil, nil
	}

	q := strings.ToLower(strings.TrimSpace(countryName))

	// Pass 2 — exact name match
	for i, s := range stats {
		if strings.ToLower(s.Country) == q {
			return &stats[i], nil
		}
	}

	// Pass 3 — disease.sh name starts with query
	for i, s := range stats {
		if strings.HasPrefix(strings.ToLower(s.Country), q) {
			return &stats[i], nil
		}
	}

	// Pass 4 — query starts with disease.sh name
	for i, s := range stats {
		name := strings.ToLower(s.Country)
		if strings.HasPrefix(q, name) && len(name) >= 4 {
			return &stats[i], nil
		}
	}

	// Pass 5 — query covers >60% of name
	for i, s := range stats {
		name := strings.ToLower(s.Country)
		if len(q) >= 4 && strings.Contains(name, q) &&
			float64(len(q))/float64(len(name)) > 0.6 {
			return &stats[i], nil
		}
	}

	// Pass 6 — name inside query - not required right now
	// for i, s := range stats {
	// 	name := strings.ToLower(s.Country)
	// 	if len(name) >= 5 && strings.Contains(q, name) {
	// 		return &stats[i], nil
	// 	}
	// }

	return nil, nil
}

// ── WHO Outbreaks ─────────────────────────────────────────────────────────────

func (f *Fetcher) Outbreaks() ([]Outbreak, error) {
	if data, ok := f.outbreakCache.get(); ok {
		return data, nil
	}

	var raw whoResponse
	if err := f.fetchJSON(whoURL, &raw); err != nil {
		return []Outbreak{}, nil // WHO is optional
	}

	outbreaks := make([]Outbreak, 0, len(raw.Value))
	for _, v := range raw.Value {
		summary := v.Summary
		if len(summary) > 300 {
			summary = summary[:300]
		}
		outbreaks = append(outbreaks, Outbreak{
			Title:   v.Title,
			Date:    v.PublicationDateAndTime,
			UrlName: v.UrlName,
			Summary: summary,
		})
	}

	f.outbreakCache.set(outbreaks)
	return outbreaks, nil
}

func (f *Fetcher) FindOutbreaks(countryName string) ([]Outbreak, error) {
	all, err := f.Outbreaks()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(countryName))
	var result []Outbreak
	for _, o := range all {
		if strings.Contains(strings.ToLower(o.Title), q) ||
			strings.Contains(strings.ToLower(o.Summary), q) {
			result = append(result, o)
			if len(result) >= 5 {
				break
			}
		}
	}
	return result, nil
}