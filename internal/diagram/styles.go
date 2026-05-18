package diagram

// Style defines the visual properties for a diagram element.
type Style struct {
	FillR, FillG, FillB          float64
	FillAlpha                    float64
	OutlineR, OutlineG, OutlineB float64
	TextR, TextG, TextB          float64
	FontFamily                   string
	FontSize                     float64
	HasFill                      bool
}

var styles = map[string]Style{
	"primary": {
		FillR: 0.0, FillG: 0.231, FillB: 0.361, FillAlpha: 1,
		OutlineR: 0.0, OutlineG: 0.231, OutlineB: 0.361,
		TextR: 1, TextG: 1, TextB: 1,
		FontFamily: "Roboto", FontSize: 11, HasFill: true,
	},
	"secondary": {
		FillR: 0.290, FillG: 0.565, FillB: 0.851, FillAlpha: 1,
		OutlineR: 0.290, OutlineG: 0.565, OutlineB: 0.851,
		TextR: 1, TextG: 1, TextB: 1,
		FontFamily: "Roboto", FontSize: 11, HasFill: true,
	},
	"accent": {
		FillR: 0.914, FillG: 0.306, FillB: 0.106, FillAlpha: 1,
		OutlineR: 0.914, OutlineG: 0.306, OutlineB: 0.106,
		TextR: 1, TextG: 1, TextB: 1,
		FontFamily: "Roboto", FontSize: 11, HasFill: true,
	},
	"neutral": {
		FillR: 0.941, FillG: 0.941, FillB: 0.941, FillAlpha: 1,
		OutlineR: 0.800, OutlineG: 0.800, OutlineB: 0.800,
		TextR: 0.2, TextG: 0.2, TextB: 0.2,
		FontFamily: "Roboto", FontSize: 11, HasFill: true,
	},
	"highlight": {
		FillR: 0.153, FillG: 0.682, FillB: 0.376, FillAlpha: 1,
		OutlineR: 0.153, OutlineG: 0.682, OutlineB: 0.376,
		TextR: 1, TextG: 1, TextB: 1,
		FontFamily: "Roboto", FontSize: 11, HasFill: true,
	},
	"outline_only": {
		FillAlpha: 0, HasFill: false,
		OutlineR: 0.0, OutlineG: 0.231, OutlineB: 0.361,
		TextR: 0.2, TextG: 0.2, TextB: 0.2,
		FontFamily: "Roboto", FontSize: 11,
	},
}

// groupStyle is used for group background rectangles.
var groupStyle = Style{
	FillR: 0.941, FillG: 0.953, FillB: 0.973, FillAlpha: 0.5,
	OutlineR: 0.800, OutlineG: 0.820, OutlineB: 0.850,
	TextR: 0.4, TextG: 0.4, TextB: 0.4,
	FontFamily: "Roboto", FontSize: 10, HasFill: true,
}

// LookupStyle returns the style for the given name, falling back to "neutral".
func LookupStyle(name string) Style {
	if s, ok := styles[name]; ok {
		return s
	}
	return styles["neutral"]
}

// LookupGroupStyle returns the style for the given group name, using the
// group-specific base style when no override is specified.
func LookupGroupStyle(name string) Style {
	if name == "" {
		return groupStyle
	}
	if s, ok := styles[name]; ok {
		s.FillAlpha = 0.3
		return s
	}
	return groupStyle
}
