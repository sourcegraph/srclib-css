.PHONY: install

install: .bin/srclib-css

.bin/srclib-css:
	go build -o .bin/srclib-css
