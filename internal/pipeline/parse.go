package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/cooking-recipe/cooking-recipe-service/internal/db"
)

// ParseComposed extracts a ComposedRecipe out of raw LLM bytes. We tolerate
// the model occasionally wrapping JSON in ```json fences.
func ParseComposed(raw []byte) (*db.ComposedRecipe, error) {
	cleaned := stripJSONFences(raw)

	var r db.ComposedRecipe
	if err := json.Unmarshal(cleaned, &r); err != nil {
		return nil, fmt.Errorf("decode json: %w (raw: %s)", err, truncate(string(cleaned), 500))
	}
	if r.Title == "" || len(r.Steps) == 0 {
		return nil, fmt.Errorf("recipe missing required fields")
	}
	if r.TotalTimeMin == 0 {
		r.TotalTimeMin = r.PrepTimeMin + r.CookTimeMin
	}
	return &r, nil
}

// stripJSONFences removes a leading "```json\n" / trailing "```" wrapper if present.
func stripJSONFences(b []byte) []byte {
	b = bytes.TrimSpace(b)
	if !bytes.HasPrefix(b, []byte("```")) {
		return b
	}
	// strip first line
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		b = b[i+1:]
	}
	b = bytes.TrimRight(b, "` \n")
	return b
}
