BIN := tangsible

.PHONY: build
build:
	go build -o $(BIN) .

# Render every screenrecordings/*.tape into screenrecordings/output/ (not
# committed). Needs vhs, ttyd and ffmpeg; uses the freshly built binary.
.PHONY: demos
demos: build
	@command -v vhs >/dev/null || { echo "vhs not found (https://github.com/charmbracelet/vhs)"; exit 1; }
	@mkdir -p screenrecordings/output
	@for t in screenrecordings/*.tape; do \
		echo "==> $$t"; PATH="$(CURDIR):$$PATH" vhs "$$t" || exit 1; \
	done

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
