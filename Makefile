BIN := tangsible

.PHONY: build
build:
	go build -o $(BIN) .

# Render every screenrecordings/*.tape into screenrecordings/output/ (not
# committed), plus a poster PNG for each. Needs vhs, ttyd and ffmpeg, and for
# the ee tape podman, ansible-builder and network access (render.sh); uses
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

# Run the callback (Python), unit and e2e tests against another ansible-core:
# `make test-ansible VERSION=2.20` (see scripts/test-ansible.sh for the
# SUITES / ISOLATE / REFRESH options and what it does not cover).
.PHONY: test-ansible
test-ansible:
	@test -n "$(VERSION)" || { echo "usage: make test-ansible VERSION=2.20"; exit 2; }
	@scripts/test-ansible.sh $(VERSION)
