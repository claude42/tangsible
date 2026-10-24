BIN := tangsible

.PHONY: build
build:
	go build -o $(BIN) .

.PHONY: install
install: build
	./install.sh --local -y

.PHONY: uninstall
uninstall:
	./install.sh --uninstall -y

.PHONY: clean
clean:
	rm -f $(BIN)

MANDIR := man
MANSRC := $(wildcard $(MANDIR)/*.1.scd)
MANOUT := $(MANSRC:.1.scd=.1)

.PHONY: man
man: $(MANOUT)

$(MANDIR)/%.1: $(MANDIR)/%.1.scd
	scdoc < $< > $@

.PHONY: clean-man
clean-man:
	rm -f $(MANOUT)
