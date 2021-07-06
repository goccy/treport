# treport

A fast scalable repository scanning tool

# Status

Under development...

# Features

- Scan files existing in the repository with arbitrary logic
- You can choose what to scan from all commits, merge commits, or heads only
- Scanning logic can be developed in multiple languages
- Scanning logic can be provided as a gRPC based plugin
- Scan results by each plugin can be typed on a protocol buffer basis and can be type-safely referenced by all plugins
- Pipeline processing that combines plugins
- Scalable
- Caching for the scan results
- Various output formats
- Declarative description of plugins in YAML
