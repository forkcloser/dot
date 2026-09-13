# This file is the project's own — add recipes below. Keep the import: it
# mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

# github.com/golang/freetype (reached through go-graphviz) ships its own
# FreeType-or-GPLv2 dual licence text, which go-licenses cannot classify. This
# project takes it on the FreeType terms; see README, "Licence".
export LINT_GO_LICENSES_FLAGS := '--ignore=github.com/golang/freetype'

# The FIRST recipe defined here becomes `just`'s default.
lint: do::lint::go::default do::lint::go::deadcode do::lint::default
fix: do::fix::go::default do::fix::default
test: do::test::go::unit do::test::go::race
