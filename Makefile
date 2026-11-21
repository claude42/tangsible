BIN := tangsible

.PHONY: build
build:
	go build -o $(BIN) .

# Render every screenrecordings/*.tape into screenrecordings/output/ (not
# committed), plus a poster PNG for each. Needs vhs, ttyd and ffmpeg, and for
# the ee-* tapes podman, ansible-builder and network access (render.sh); uses
# the freshly built binary. `screenrecordings/render.sh stats hosts` renders
# only the tapes named.
.PHONY: demos
demos:
	@command -v vhs >/dev/null || { echo "vhs not found (https://github.com/charmbracelet/vhs)"; exit 1; }
	@mkdir -p screenrecordings/output
	@screenrecordings/render.sh

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
