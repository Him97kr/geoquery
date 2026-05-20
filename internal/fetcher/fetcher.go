// internal/fetcher/fetcher.go
// Fetches and caches data from:
//   - REST Countries v4 API  (population, density, region, etc.)
//   - disease.sh API         (COVID-19 stats)
//   - WHO Outbreak News API  (disease alerts)

package fetcher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	restCountriesURL = "https://restcountries.com/v4/all?fields=name,cca3,capital,region,population,density,area,flag,languages,currencies"
	covidURL         = "https://disease.sh/v3/covid-19/countries?allowNull=true"
	whoURL           = "https://www.who.int/api/news/diseaseoutbreaknews?sf_culture=en&$top=100&$orderby=PublicationDateAndTime%20desc"
	cacheTTL         = 30 * time.Minute
)

// ── Raw API structs ───────────────────────────────────────────────────────────

type restCountry struct {
	Name struct {
		Common   string `json:"common"`
		Official string `json:"official"`
	} `json:"name"`
	CCA3      string   `json:"cca3"`
	Capital   []string `json:"capital"`   // v4: array
	Region    string   `json:"region"`
	Population int     `json:"population"`
	Density   float64  `json:"density"`
	Area      float64  `json:"area"`
	Flag struct {
		Emoji string `json:"emoji"`       // v4: object with emoji field
		PNG   string `json:"png"`
	} `json:"flag"`
	Languages []struct {
		Name   string `json:"name"`
	} `json:"languages"`  // v4: map
	Currencies []struct {
		Name   string `json:"name"`
		Symbol string `json:"symbol"`
	} `json:"currencies"` // v4: array
}

type covidCountry struct {
	Country          string  `json:"country"`
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
		Url                    string `json:"Url"`
		UrlAlias               string `json:"UrlAlias"`
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
	URL     string
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

	var raw []restCountry
	if err := f.fetchJSON(restCountriesURL, &raw); err != nil {
		return nil, err
	}

	countries := make([]Country, 0, len(raw))
	for _, r := range raw {
		// capital is []string in v4
		capital := ""
		if len(r.Capital) > 0 {
			capital = r.Capital[0]
		}

		// languages is []{name} in v4
		langs := make([]string, 0, len(r.Languages))
		for _, v := range r.Languages {
			langs = append(langs, v.Name)
		}

		// currencies is []{name,symbol} in v4
		currs := make([]string, 0, len(r.Currencies))
		for _, v := range r.Currencies {
			currs = append(currs, v.Name)
		}

		// flag: use emoji from flag object
		flag := r.Flag.Emoji

		countries = append(countries, Country{
			Name:       r.Name.Common,
			Code:       r.CCA3,
			Capital:    capital,
			Region:     r.Region,
			Population: r.Population,
			Density:    r.Density,
			Area:       r.Area,
			Flag:       flag,
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

	for i, c := range countries {
		if qc != "" && strings.EqualFold(c.Code, qc) {
			return &countries[i], nil
		}
		if q != "" {
			cname := strings.ToLower(c.Name)
			if cname == q || strings.Contains(cname, q) || strings.Contains(q, cname) {
				return &countries[i], nil
			}
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
	stats, err := f.CovidStats()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(countryName))
	for i, s := range stats {
		name := strings.ToLower(s.Country)
		if name == q || strings.Contains(name, q) || strings.Contains(q, name) {
			return &stats[i], nil
		}
	}
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
		url := v.Url
		if url == "" {
			url = v.UrlAlias
		}
		summary := v.Summary
		if len(summary) > 300 {
			summary = summary[:300]
		}
		outbreaks = append(outbreaks, Outbreak{
			Title:   v.Title,
			Date:    v.PublicationDateAndTime,
			URL:     url,
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
