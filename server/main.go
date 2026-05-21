// server/main.go
package main

import (
	"log"
	"net/http"
	"os"

	graphql "github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/rs/cors"

	"github.com/Him97kr/geoquery/graph/resolver"
	"github.com/Him97kr/geoquery/internal/fetcher"
)

// schema defined inline — no separate package needed
var schemaString = `
	type Country {
		name:       String!
		code:       String!
		capital:    String
		region:     String
		population: Int
		density:    Float
		area:       Float
		flag:       String
		languages:  [String!]!
		currencies: [String!]!
		covid:      CovidStats
		outbreaks:  [Outbreak!]!
	}

	type CovidStats {
		country:          String!
		cases:            Int
		todayCases:       Int
		deaths:           Int
		todayDeaths:      Int
		recovered:        Int
		active:           Int
		critical:         Int
		casesPerMillion:  Float
		deathsPerMillion: Float
		tests:            Int
		updatedAt:        String
	}

	type Outbreak {
		title:   String!
		date:    String
		urlName: String
		summary: String
	}

	type GlobalStats {
		totalCountries:   Int!
		totalPopulation:  String!
		totalCovidCases:  Int!
		totalCovidDeaths: Int!
		totalActive:      Int!
		mostPopulated:    Country
		leastPopulated:   Country
		highestDensity:   Country
		mostCovidCases:   Country
	}

	type Query {
		country(name: String, code: String): Country
		countries(region: String, minPop: Int, maxPop: Int, limit: Int): [Country!]!
		searchCountries(query: String!): [Country!]!
		globalStats: GlobalStats!
		topByPopulation(limit: Int): [Country!]!
		topByCovid(limit: Int): [Country!]!
		countriesWithOutbreaks: [Country!]!
	}
`

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	f := fetcher.New()

	s := graphql.MustParseSchema(schemaString, &resolver.Resolver{Fetcher: f})

	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
	})

	mux := http.NewServeMux()
	mux.Handle("/graphql", &relay.Handler{Schema: s})
	mux.HandleFunc("/playground", playgroundHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"geoquery"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/playground", http.StatusFound)
	})

	log.Printf("🌍 GeoQuery running on http://localhost:%s", port)
	log.Printf("📊 Playground: http://localhost:%s/playground", port)
	log.Printf("🔌 GraphQL:    http://localhost:%s/graphql", port)

	if err := http.ListenAndServe(":"+port, c.Handler(mux)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func playgroundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
  <title>GeoQuery Playground</title>
  <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/graphiql/3.0.9/graphiql.min.css"/>
</head>
<body style="margin:0">
  <div id="graphiql" style="height:100vh"></div>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/react/18.2.0/umd/react.production.min.js"></script>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/react-dom/18.2.0/umd/react-dom.production.min.js"></script>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/graphiql/3.0.9/graphiql.min.js"></script>
  <script>
    const fetcher = GraphiQL.createFetcher({ url: '/graphql' });
    ReactDOM.render(
      React.createElement(GraphiQL, { fetcher }),
      document.getElementById('graphiql')
    );
  </script>
</body>
</html>`))
}
