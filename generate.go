package main

// Regenera el recurso de Windows (ícono del .exe + metadatos de autor) desde
// icon.ico + versioninfo.json. Requiere:
//   go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
// y luego:  go generate ./...
//
//go:generate goversioninfo -o resource_windows_amd64.syso -icon icon.ico versioninfo.json
