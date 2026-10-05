# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/): the versioned surface is the
command line interface described in `README.md`.

## [Unreleased]

### Changed

- The engine is forkcloser/go-graphviz v0.3.0, forkcloser's fork of
  goccy/go-graphviz, in place of goccy/go-graphviz v0.2.10. It carries
  Graphviz 16.1.0 where v0.2.10 carried 12.1.2, so layouts and renders follow
  Graphviz's own changes between the two, and the fork's own fixes are in its
  [CHANGELOG](https://github.com/forkcloser/go-graphviz/blob/v0.3.0/CHANGELOG.md);
  the command, its flags and its output formats are unchanged. The fork no longer pulls in
  disintegration/imaging or flopp/go-findfont.

## [1.0.1] - 2026-09-20

### Fixed

- `-o` rendered straight into the output path, so a failed render, an
  interrupt or a full disk left a truncated file there; a plain parse error
  emptied a previously good output. The render now goes to a temporary file
  beside the target, renamed onto it only on success.

## [1.0.0] - 2026-09-13

### Added

- The `dot` command: `-K` layout, `-T` format (dot, svg, png, jpg), `-o`
  output, file or standard input, file or standard output, Graphviz's glued
  and spaced flag forms, `-V` from the build info. Rendering by go-graphviz
  v0.2.10, Graphviz as WebAssembly.
- Empty or whitespace-only input is reported as "no graph found" instead of
  faulting inside the engine.
