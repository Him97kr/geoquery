// graph/resolver/resolver.go
// Resolvers for graph-gophers/graphql-go
// Only imports: context, strings, and internal/fetcher

package resolver

import (
	"context"
	"strings"
	"fmt"

	"github.com/Him97kr/geoquery/internal/fetcher"
)

// ── Root resolver ─────────────────────────────────────────────────────────────
type Resolver struct {
	Fetcher *fetcher.Fetcher
}

// ── Argument structs ──────────────────────────────────────────────────────────
type countryArgs   struct { Name *string; Code *string }
type countriesArgs struct { Region *string; MinPop *int32; MaxPop *int32; Limit *int32 }
type searchArgs    struct { Query string }
type limitArgs     struct { Limit *int32 }

// ── Country resolver ──────────────────────────────────────────────────────────
type countryResolver struct {
	c       fetcher.Country
	fetcher *fetcher.Fetcher
}

func (r *countryResolver) Name()       string   { return r.c.Name }
func (r *countryResolver) Code()       string   { return r.c.Code }
func (r *countryResolver) Capital()    *string  { return strPtr(r.c.Capital) }
func (r *countryResolver) Region()     *string  { return strPtr(r.c.Region) }
func (r *countryResolver) Population() *int32   { v := int32(r.c.Population); return &v }
func (r *countryResolver) Density()    *float64 { return &r.c.Density }
func (r *countryResolver) Area()       *float64 { return &r.c.Area }
func (r *countryResolver) Flag()       *string  { return strPtr(r.c.Flag) }
func (r *countryResolver) Languages()  []string { return r.c.Languages }
func (r *countryResolver) Currencies() []string { return r.c.Currencies }

func (r *countryResolver) Covid() *covidResolver {
	stats, _ := r.fetcher.FindCovid(r.c.Name)
	if stats == nil { return nil }
	return &covidResolver{s: *stats}
}

func (r *countryResolver) Outbreaks() []*outbreakResolver {
	os, _ := r.fetcher.FindOutbreaks(r.c.Name)
	return toOutbreakResolvers(os)
}

// ── COVID resolver ────────────────────────────────────────────────────────────
type covidResolver struct{ s fetcher.CovidStats }

func (r *covidResolver) Country()          string   { return r.s.Country }
func (r *covidResolver) Cases()            *int32   { v := int32(r.s.Cases); return &v }
func (r *covidResolver) TodayCases()       *int32   { v := int32(r.s.TodayCases); return &v }
func (r *covidResolver) Deaths()           *int32   { v := int32(r.s.Deaths); return &v }
func (r *covidResolver) TodayDeaths()      *int32   { v := int32(r.s.TodayDeaths); return &v }
func (r *covidResolver) Recovered()        *int32   { v := int32(r.s.Recovered); return &v }
func (r *covidResolver) Active()           *int32   { v := int32(r.s.Active); return &v }
func (r *covidResolver) Critical()         *int32   { v := int32(r.s.Critical); return &v }
func (r *covidResolver) CasesPerMillion()  *float64 { return &r.s.CasesPerMillion }
func (r *covidResolver) DeathsPerMillion() *float64 { return &r.s.DeathsPerMillion }
func (r *covidResolver) Tests()            *int32   { v := int32(r.s.Tests); return &v }
func (r *covidResolver) UpdatedAt()        *string  { return strPtr(r.s.UpdatedAt) }

// ── Outbreak resolver ─────────────────────────────────────────────────────────
type outbreakResolver struct{ o fetcher.Outbreak }

func (r *outbreakResolver) Title()   string  { return r.o.Title }
func (r *outbreakResolver) Date()    *string { return strPtr(r.o.Date) }
func (r *outbreakResolver) UrlName() *string { return strPtr(r.o.UrlName) }
func (r *outbreakResolver) Summary() *string { return strPtr(r.o.Summary) }

// ── GlobalStats resolver ──────────────────────────────────────────────────────
type globalStatsResolver struct {
	totalCountries   int32
	totalPopulation  int64
	totalCovidCases  int32
	totalCovidDeaths int32
	totalActive      int32
	mostPopulated    *countryResolver
	leastPopulated   *countryResolver
	highestDensity   *countryResolver
	mostCovidCases   *countryResolver
}

func (r *globalStatsResolver) TotalCountries()   int32            { return r.totalCountries }
func (r *globalStatsResolver) TotalPopulation()  string           { return fmt.Sprintf("%d", r.totalPopulation) }
func (r *globalStatsResolver) TotalCovidCases()  int32            { return r.totalCovidCases }
func (r *globalStatsResolver) TotalCovidDeaths() int32            { return r.totalCovidDeaths }
func (r *globalStatsResolver) TotalActive()      int32            { return r.totalActive }
func (r *globalStatsResolver) MostPopulated()    *countryResolver { return r.mostPopulated }
func (r *globalStatsResolver) LeastPopulated()   *countryResolver { return r.leastPopulated }
func (r *globalStatsResolver) HighestDensity()   *countryResolver { return r.highestDensity }
func (r *globalStatsResolver) MostCovidCases()   *countryResolver { return r.mostCovidCases }

// ── Query resolvers ───────────────────────────────────────────────────────────

func (r *Resolver) Country(ctx context.Context, args countryArgs) (*countryResolver, error) {
	n, c := "", ""
	if args.Name != nil { n = *args.Name }
	if args.Code != nil { c = *args.Code }
	found, err := r.Fetcher.FindCountry(n, c)
	if err != nil || found == nil { return nil, err }
	return &countryResolver{c: *found, fetcher: r.Fetcher}, nil
}

func (r *Resolver) Countries(ctx context.Context, args countriesArgs) ([]*countryResolver, error) {
	all, err := r.Fetcher.Countries()
	if err != nil { return nil, err }
	var result []*countryResolver
	for _, c := range all {
		c := c
		if args.Region != nil    && !strings.EqualFold(c.Region, *args.Region)       { continue }
		if args.MinPop != nil    && int32(c.Population) < *args.MinPop               { continue }
		if args.MaxPop != nil    && int32(c.Population) > *args.MaxPop               { continue }
		result = append(result, &countryResolver{c: c, fetcher: r.Fetcher})
		if args.Limit != nil && int32(len(result)) >= *args.Limit { break }
	}
	return result, nil
}

func (r *Resolver) SearchCountries(ctx context.Context, args searchArgs) ([]*countryResolver, error) {
	all, err := r.Fetcher.Countries()
	if err != nil { return nil, err }
	q := strings.ToLower(strings.TrimSpace(args.Query))
	var result []*countryResolver
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Name), q) {
			c := c
			result = append(result, &countryResolver{c: c, fetcher: r.Fetcher})
		}
	}
	return result, nil
}

func (r *Resolver) GlobalStats(ctx context.Context) (*globalStatsResolver, error) {
	countries, err := r.Fetcher.Countries()
	if err != nil { return nil, err }
	covidStats, _ := r.Fetcher.CovidStats()

	covidMap := map[string]*fetcher.CovidStats{}
	for i, s := range covidStats {
		covidMap[strings.ToLower(s.Country)] = &covidStats[i]
	}

	var totalPop int64
	var totalCases, totalDeaths, totalActive int32
	var mostPop, leastPop, highDensity, mostCovid *fetcher.Country

	for i, c := range countries {
		c := c
		totalPop += int64(c.Population)
		if mostPop == nil || c.Population > mostPop.Population                         { mostPop = &countries[i] }
		if leastPop == nil || (c.Population > 0 && c.Population < leastPop.Population) { leastPop = &countries[i] }
		if highDensity == nil || c.Density > highDensity.Density                       { highDensity = &countries[i] }
		if cv, ok := covidMap[strings.ToLower(c.Name)]; ok {
			totalCases  += int32(cv.Cases)
			totalDeaths += int32(cv.Deaths)
			totalActive += int32(cv.Active)
			if mostCovid == nil {
				mostCovid = &countries[i]
			} else if cvMost := covidMap[strings.ToLower(mostCovid.Name)]; cvMost != nil && cv.Cases > cvMost.Cases {
				mostCovid = &countries[i]
			}
		}
	}

	toRes := func(c *fetcher.Country) *countryResolver {
		if c == nil { return nil }
		return &countryResolver{c: *c, fetcher: r.Fetcher}
	}

	return &globalStatsResolver{
		totalCountries:   int32(len(countries)),
		totalPopulation:  totalPop,
		totalCovidCases:  totalCases,
		totalCovidDeaths: totalDeaths,
		totalActive:      totalActive,
		mostPopulated:    toRes(mostPop),
		leastPopulated:   toRes(leastPop),
		highestDensity:   toRes(highDensity),
		mostCovidCases:   toRes(mostCovid),
	}, nil
}

func (r *Resolver) TopByPopulation(ctx context.Context, args limitArgs) ([]*countryResolver, error) {
	all, err := r.Fetcher.Countries()
	if err != nil { return nil, err }
	n := 10
	if args.Limit != nil { n = int(*args.Limit) }
	sorted := make([]fetcher.Country, len(all))
	copy(sorted, all)
	for i := 0; i < n && i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Population > sorted[i].Population { sorted[i], sorted[j] = sorted[j], sorted[i] }
		}
	}
	result := make([]*countryResolver, 0, n)
	for i := 0; i < n && i < len(sorted); i++ {
		c := sorted[i]
		result = append(result, &countryResolver{c: c, fetcher: r.Fetcher})
	}
	return result, nil
}

func (r *Resolver) TopByCovid(ctx context.Context, args limitArgs) ([]*countryResolver, error) {
	covidStats, err := r.Fetcher.CovidStats()
	if err != nil { return nil, err }
	n := 10
	if args.Limit != nil { n = int(*args.Limit) }
	sorted := make([]fetcher.CovidStats, len(covidStats))
	copy(sorted, covidStats)
	for i := 0; i < n && i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Cases > sorted[i].Cases { sorted[i], sorted[j] = sorted[j], sorted[i] }
		}
	}
	result := make([]*countryResolver, 0, n)
	for i := 0; i < n && i < len(sorted); i++ {
		found, _ := r.Fetcher.FindCountry(sorted[i].Country, "")
		if found != nil {
			result = append(result, &countryResolver{c: *found, fetcher: r.Fetcher})
		}
	}
	return result, nil
}

func (r *Resolver) CountriesWithOutbreaks(ctx context.Context) ([]*countryResolver, error) {
	outbreaks, err := r.Fetcher.Outbreaks()
	if err != nil { return nil, err }
	seen := map[string]bool{}
	var result []*countryResolver
	for _, o := range outbreaks {
		found, _ := r.Fetcher.FindCountry(o.Title, "")
		if found == nil { continue }
		key := strings.ToLower(found.Name)
		if seen[key] { continue }
		seen[key] = true
		result = append(result, &countryResolver{c: *found, fetcher: r.Fetcher})
	}
	return result, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────
func strPtr(s string) *string {
	if s == "" { return nil }
	return &s
}

func toOutbreakResolvers(os []fetcher.Outbreak) []*outbreakResolver {
	result := make([]*outbreakResolver, 0, len(os))
	for _, o := range os {
		o := o
		result = append(result, &outbreakResolver{o: o})
	}
	return result
}
