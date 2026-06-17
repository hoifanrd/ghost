package output

// Capabilities is what `ghost capabilities` prints, letting core probe a base
// image's ghost version at executor creation: schema_max bounds the trailer
// schema core can parse, and features gates supervise-vs-fallback.
type Capabilities struct {
	SchemaMax int      `json:"schema_max"`
	Features  []string `json:"features"`
}

// Current returns the capabilities this ghost build advertises. One source of
// truth for the feature list. result-trailer and supervise are the tokens core
// matches on to decide whether to use the supervise argv; the rest are
// informational/forward-looking.
func Current() Capabilities {
	return Capabilities{
		SchemaMax: TrailerSchema,
		Features: []string{
			"supervise",
			"result-trailer",
			"peak-memory-sampling",
			"oom-attribution",
			"output-cap",
			"max-pids",
		},
	}
}
