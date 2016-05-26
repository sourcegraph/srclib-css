ifeq (${OS},Windows_NT)
	EXE := .bin/srclib-css.exe
else
	EXE := .bin/srclib-css
endif

.PHONY: install clean

default: govendor install

install: ${EXE}

clean:
	rm ${EXE}

govendor:
	go get github.com/kardianos/govendor
	govendor sync

${EXE}:
	go build -o ${EXE}
