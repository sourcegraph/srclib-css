.PHONY: install clean

default: govendor install

install: .bin/srclib-css

clean:
	rm .bin/srclib-css

govendor:
	go get github.com/kardianos/govendor
	govendor sync

.bin/srclib-css:
	go build -o .bin/srclib-css
