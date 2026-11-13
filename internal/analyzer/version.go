package analyzer

import (
	"encoding/json"

	"github.com/VaDKustiK/ytaudio/packaging"
)

type versionMetadata struct {
	Schema int    `json:"schema_version"`
	Engine string `json:"engine_version"`
	Models string `json:"models_version"`
}

var componentVersion = func() versionMetadata {
	var value versionMetadata
	if err := json.Unmarshal(packaging.AnalyzerMetadata, &value); err != nil || value.Schema != 1 || value.Engine == "" || value.Models == "" {
		panic("invalid embedded analyzer version metadata")
	}
	return value
}()

var Version = componentVersion.Engine
var ModelsVersion = componentVersion.Models

const manifestVersion = "1"
