module github.com/GustavoCaso/n2o

go 1.18

require (
	github.com/dstotijn/go-notion v0.11.0
	github.com/itchyny/timefmt-go v0.1.5
	github.com/schollz/progressbar/v3 v3.14.1
)

require (
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/rivo/uniseg v0.4.4 // indirect
	golang.org/x/sys v0.14.0 // indirect
	golang.org/x/term v0.14.0 // indirect
	golang.org/x/xerrors v0.0.0-20231012003039-104605ab7028 // indirect
)

replace github.com/GustavoCaso/n2o/internal/queue => ../internal/greetings
