.PHONY: install clean

install: .bin/srclib-css

clean:
	rm .bin/srclib-css

.bin/srclib-css:
	go build -o .bin/srclib-css
