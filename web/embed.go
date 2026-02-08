package webassets

import "embed"

//go:embed templates/*.html
var TemplatesFS embed.FS

//go:embed static/*
var StaticFS embed.FS

//go:embed i18n/*.json
var I18nFS embed.FS
