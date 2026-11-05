package plugin

import (
	_ "embed"
	"encoding/json"
	"html"
	"strings"
)

//go:embed web/ui.html
var uiTemplate string

//go:embed web/style.css
var uiStyles string

//go:embed web/theme.css
var uiThemes string

//go:embed web/vendor/markdown-it.min.js
var markdownScript string

//go:embed web/vendor/markdown-it-cjk-friendly.min.js
var cjkFriendlyScript string

//go:embed web/vendor/texmath.js
var texmathScript string

//go:embed web/vendor/temml.min.js
var temmlScript string

//go:embed web/vendor/temml.css
var temmlStyles string

//go:embed web/storage.js
var storageScript string

//go:embed web/host.js
var hostScript string

//go:embed web/providers.js
var providerScript string

//go:embed web/credentials.js
var credentialScript string

//go:embed web/app.js
var appScript string

//go:embed web/catalog.js
var catalogScript string

//go:embed web/components.js
var componentsScript string

//go:embed web/candy.js
var candyScript string

//go:embed web/siwc.js
var siwcScript string

//go:embed web/fingerprint.js
var fingerprintScript string

//go:embed web/pelican.js
var pelicanScript string

var uiHTML = func() []byte {
	config, _ := json.Marshal(map[string]any{"requests": fingerprintTarget, "aliases": fingerprintAliases})
	fingerprint := strings.Replace(fingerprintScript, `/*FINGERPRINT_CONFIG*/{}`, string(config), 1)
	variants, _ := json.Marshal(pelicanVariants)
	pelican := strings.Replace(pelicanScript, `/*PELICAN_VARIANTS*/[]`, string(variants), 1)
	return []byte(strings.NewReplacer(
		"/*APP_STYLES*/", temmlStyles+"\n"+uiThemes+"\n"+uiStyles,
		"<!--PLUGIN_VERSION-->", pluginVersion,
		"<!--CANDY_PROMPT-->", html.EscapeString(candyPrompt),
		"<!--DETECTOR_LICENSE-->", "<!-- LM Fingerpoint Detector\n"+detectorLicense+"-->",
		"/*MARKDOWN_SCRIPT*/", markdownScript,
		"/*CJK_FRIENDLY_SCRIPT*/", cjkFriendlyScript,
		"/*TEXMATH_SCRIPT*/", texmathScript,
		"/*TEMML_SCRIPT*/", temmlScript,
		"/*STORAGE_SCRIPT*/", storageScript,
		"/*HOST_SCRIPT*/", hostScript,
		"/*PROVIDERS_SCRIPT*/", providerScript,
		"/*CREDENTIALS_SCRIPT*/", credentialScript,
		"/*CATALOG_SCRIPT*/", catalogScript,
		"/*COMPONENTS_SCRIPT*/", componentsScript,
		"/*CANDY_SCRIPT*/", candyScript,
		"/*SIWC_SCRIPT*/", siwcScript,
		"/*FINGERPRINT_SCRIPT*/", fingerprint,
		"/*PELICAN_SCRIPT*/", pelican,
		"/*APP_SCRIPT*/", appScript,
	).Replace(uiTemplate))
}()

// Lucide "candy" icon. Hosts render it in an img element, so the stroke color is fixed.
const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#72787c" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><style>@media (prefers-color-scheme: dark) { :root { stroke: #9c9d9b; } }</style><path d="M10 7v10.9"/><path d="M14 6.1V17"/><path d="M16 7V3a1 1 0 0 1 1.707-.707 2.5 2.5 0 0 0 2.152.717 1 1 0 0 1 1.131 1.131 2.5 2.5 0 0 0 .717 2.152A1 1 0 0 1 21 8h-4"/><path d="M16.536 7.465a5 5 0 0 0-7.072 0l-2 2a5 5 0 0 0 0 7.07 5 5 0 0 0 7.072 0l2-2a5 5 0 0 0 0-7.07"/><path d="M8 17v4a1 1 0 0 1-1.707.707 2.5 2.5 0 0 0-2.152-.717 1 1 0 0 1-1.131-1.131 2.5 2.5 0 0 0-.717-2.152A1 1 0 0 1 3 16h4"/></svg>`
