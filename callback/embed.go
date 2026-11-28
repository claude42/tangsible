// Copyright 2026 Klaus Wissmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package callback holds tangsible's ansible callback plugin
// (design-docs/OwnCallbackPlugin.md) so it can be compiled into the binary:
// go:embed only reaches files below the declaring package's own directory.
package callback

import _ "embed"

// Plugin is the source of tangsible_events.py.
//
//go:embed tangsible_events.py
var Plugin []byte
