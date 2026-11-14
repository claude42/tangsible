# Copyright 2026 Klaus Wissmann
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Action plugin behind the host verb's Variables tab.

It runs on the control node and reports the variables defined for the host it
runs for, with their origins (see ``tangsible_variables`` in the callback
plugin, which does the work). It only reads; it never touches the host.
"""

from __future__ import annotations

import sys

from ansible.plugins.action import ActionBase

_HELPER_MODULE = "ansible.plugins.callback.tangsible_events"


class ActionModule(ActionBase):
    TRANSFERS_FILES = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        helper = sys.modules.get(_HELPER_MODULE)
        if helper is None:
            result["failed"] = True
            result["msg"] = "tangsible_vars needs tangsible's callback plugin to be loaded"
            return result
        result["changed"] = False
        stub_path = self._task.get_path().rsplit(":", 1)[0]
        result["tangsible_variables"] = helper.tangsible_variables(task_vars or {}, stub_path)
        return result
