package packaging

import _ "embed"

//go:embed ytaudiologo.png
var Icon []byte

//go:embed analyzer.json
var AnalyzerMetadata []byte
