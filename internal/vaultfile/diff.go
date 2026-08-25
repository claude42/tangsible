package vaultfile

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type decisionKind int

const (
	decisionPlain decisionKind = iota
	decisionSplice
	decisionReencrypt
	decisionProblem
)

type keyDecision struct {
	kind    decisionKind
	warn    bool   // decisionPlain: emit the "not vault-encrypted" warning
	problem string // decisionProblem: message to annotate above this key
}

// decideKey is the pure per-key policy decision behind design-docs/
// Vault.md's key-set comparison (point 1) and encrypt/leave-alone rules
// (point 3). It takes no password and performs no I/O or crypto - that's
// deliberate, since it's what makes this decision testable in complete
// isolation from the rest of the package.
func decideKey(key string, valNode *yaml.Node, before DecryptedView) keyDecision {
	if hasVaultTagAnywhere(valNode) {
		return keyDecision{kind: decisionProblem, problem: fmt.Sprintf(
			"%q carries a !vault tag that wasn't there before editing - !vault values can't be typed by hand", key)}
	}

	if oldPlaintext, wasVaulted := before.VaultedBefore[key]; wasVaulted {
		if !isPlainString(valNode) {
			return keyDecision{kind: decisionProblem, problem: fmt.Sprintf(
				"%q was vault-encrypted before editing and must remain a plain string to be saved", key)}
		}
		if valNode.Value == oldPlaintext {
			// Unchanged: splice the original node back rather than
			// re-encrypting, so its ciphertext stays byte-for-byte
			// identical - the whole point of this diff (design-docs/
			// Vault.md point 1, "the salt problem").
			return keyDecision{kind: decisionSplice}
		}
		return keyDecision{kind: decisionReencrypt}
	}

	if before.PresentBefore[key] {
		// Was already plaintext before editing - always stays
		// plaintext; tangsible never auto-encrypts an existing plain
		// value on its own initiative.
		return keyDecision{kind: decisionPlain, warn: isPlainString(valNode)}
	}

	// A genuinely new key: encrypted by default (design-docs/Vault.md
	// point 3, "variables that haven't been there when opening the file
	// should be encrypted when saving") - unless it isn't a string, in
	// which case it can't be encrypted, and can't be silently left
	// plaintext either: same hard-failure rationale as a previously-
	// vaulted key turning non-string (an unencrypted secret landing on
	// disk is worse than an inconvenient failure).
	if !isPlainString(valNode) {
		return keyDecision{kind: decisionProblem, problem: fmt.Sprintf(
			"%q is new but isn't a plain string, so it can't be encrypted", key)}
	}
	return keyDecision{kind: decisionReencrypt}
}
