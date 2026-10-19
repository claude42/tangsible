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
#
# Embedded into the tangsible binary (resolve_condition.go's go:embed) and
# run as `python3 -c <this file's contents>`, never installed standalone -
# design-docs/DebugMode.md's "Resolving when:/failed_when:/.../ conditions"
# section explains why this exists at all: ansible evaluates when:/
# failed_when:/changed_when:/until: as bare Jinja expressions (no {{ }}),
# so the existing {{ }}-regex-based resolve mechanism (resolved.go's
# wrapJinjaDefaults) never touches them, and re-running the *whole*
# expression through ansible's own templating would only ever produce a
# flat true/false - throwing away exactly the "which operand was which"
# detail that makes a condition worth reading at all. This resolves only
# the variable *references* within an expression (Name/Getattr/Getitem
# chains that fully resolve to plain data), leaving every operator,
# filter, test, and literal exactly as written - real Jinja parsing via
# the jinja2 library ansible itself depends on, not a hand-rolled one.
#
# Protocol: one JSON object on stdin, {"vars": {...}, "exprs": [...]}.
# One JSON array on stdout, one resolved string per input expr, same
# order. Any expr that doesn't parse as a plain Jinja expression, or
# whose simplification hits something this script doesn't model, is
# echoed back completely unchanged - the same "best effort, silent
# fallback to the original text" contract resolved.go's own
# wrapJinjaDefaults already has for undefined variables.
import sys
import json

import jinja2
from jinja2 import nodes

_JSON_SAFE = (str, int, float, bool, type(None), dict, list)


def _try_eval(node, varsmap):
    """Fully evaluate node against varsmap if it's pure data access (a
    variable reference, or an attribute/item lookup chain rooted at one) -
    never a filter, test, call, or operator. (ok, value); ok is False for
    anything not expressible this way, including a genuinely undefined
    name or a missing attribute/key."""
    if isinstance(node, nodes.Const):
        return True, node.value
    if isinstance(node, nodes.Name) and node.ctx == "load":
        if node.name in varsmap:
            return True, varsmap[node.name]
        return False, None
    if isinstance(node, nodes.Getattr):
        ok, base = _try_eval(node.node, varsmap)
        if ok and isinstance(base, dict) and node.attr in base:
            return True, base[node.attr]
        return False, None
    if isinstance(node, nodes.Getitem):
        ok, base = _try_eval(node.node, varsmap)
        ok2, idx = _try_eval(node.arg, varsmap)
        if ok and ok2:
            if isinstance(base, dict) and idx in base:
                return True, base[idx]
            if isinstance(base, list) and isinstance(idx, int) and -len(base) <= idx < len(base):
                return True, base[idx]
        return False, None
    return False, None


def _lit(v):
    """Render a resolved Python value as Jinja/YAML-style source text."""
    if v is True:
        return "true"
    if v is False:
        return "false"
    if v is None:
        return "none"
    if isinstance(v, (int, float)):
        return repr(v)
    if isinstance(v, str):
        return "'" + v.replace("\\", "\\\\").replace("'", "\\'") + "'"
    if isinstance(v, list):
        return "[" + ", ".join(_lit(x) for x in v) + "]"
    if isinstance(v, dict):
        return "{" + ", ".join(_lit(k) + ": " + _lit(x) for k, x in v.items()) + "}"
    return repr(v)


_OPS = {
    "eq": "==", "ne": "!=", "gt": ">", "gteq": ">=",
    "lt": "<", "lteq": "<=", "in": "in", "notin": "not in",
}
_BINOPS = {
    nodes.Add: "+", nodes.Sub: "-", nodes.Mul: "*", nodes.Div: "/",
    nodes.FloorDiv: "//", nodes.Mod: "%", nodes.Pow: "**",
}


def _simplify(node, varsmap):
    """Replace every maximal pure-data-access subtree with its resolved
    Const, in place; recurse into everything else's children so a
    variable nested inside a filter/test/call/operator/literal still gets
    its own chance."""
    ok, val = _try_eval(node, varsmap)
    if ok and isinstance(val, _JSON_SAFE) and not isinstance(node, nodes.Const):
        return nodes.Const(val)
    if isinstance(node, nodes.Compare):
        node.expr = _simplify(node.expr, varsmap)
        for op in node.ops:
            op.expr = _simplify(op.expr, varsmap)
    elif isinstance(node, (nodes.And, nodes.Or) + tuple(_BINOPS)):
        node.left = _simplify(node.left, varsmap)
        node.right = _simplify(node.right, varsmap)
    elif isinstance(node, (nodes.Not, nodes.Neg, nodes.Pos)):
        node.node = _simplify(node.node, varsmap)
    elif isinstance(node, (nodes.Filter, nodes.Test)):
        if node.node is not None:
            node.node = _simplify(node.node, varsmap)
        node.args = [_simplify(a, varsmap) for a in node.args]
    elif isinstance(node, nodes.Call):
        node.node = _simplify(node.node, varsmap)
        node.args = [_simplify(a, varsmap) for a in node.args]
    elif isinstance(node, nodes.Getattr):
        node.node = _simplify(node.node, varsmap)
    elif isinstance(node, nodes.Getitem):
        node.node = _simplify(node.node, varsmap)
        node.arg = _simplify(node.arg, varsmap)
    elif isinstance(node, nodes.Concat):
        node.nodes = [_simplify(n, varsmap) for n in node.nodes]
    elif isinstance(node, (nodes.List, nodes.Tuple)):
        node.items = [_simplify(i, varsmap) for i in node.items]
    elif isinstance(node, nodes.Dict):
        for p in node.items:
            p.key = _simplify(p.key, varsmap)
            p.value = _simplify(p.value, varsmap)
    elif isinstance(node, nodes.CondExpr):
        node.test = _simplify(node.test, varsmap)
        node.expr1 = _simplify(node.expr1, varsmap)
        if node.expr2 is not None:
            node.expr2 = _simplify(node.expr2, varsmap)
    return node


def _unparse(node):
    """Render a (possibly partially simplified) Jinja expression AST back
    to source text. Covers the grammar subset when:/failed_when:/
    changed_when:/until: conditions realistically use - not a general
    Jinja unparser. Raises ValueError for anything else, caught by the
    caller as "leave this expr unresolved"."""
    if isinstance(node, nodes.Const):
        return _lit(node.value)
    if isinstance(node, nodes.Name):
        return node.name
    if isinstance(node, nodes.Getattr):
        return "%s.%s" % (_unparse(node.node), node.attr)
    if isinstance(node, nodes.Getitem):
        return "%s[%s]" % (_unparse(node.node), _unparse(node.arg))
    if isinstance(node, nodes.Compare):
        parts = [_unparse(node.expr)]
        for op in node.ops:
            parts.append(_OPS.get(op.op, op.op))
            parts.append(_unparse(op.expr))
        return " ".join(parts)
    if isinstance(node, nodes.And):
        return "%s and %s" % (_unparse(node.left), _unparse(node.right))
    if isinstance(node, nodes.Or):
        return "%s or %s" % (_unparse(node.left), _unparse(node.right))
    if isinstance(node, nodes.Not):
        inner = node.node
        if isinstance(inner, nodes.Test):
            args = "(" + ", ".join(_unparse(a) for a in inner.args) + ")" if inner.args else ""
            return "%s is not %s%s" % (_unparse(inner.node), inner.name, args)
        return "not %s" % _unparse(inner)
    if isinstance(node, nodes.Test):
        args = "(" + ", ".join(_unparse(a) for a in node.args) + ")" if node.args else ""
        return "%s is %s%s" % (_unparse(node.node), node.name, args)
    if isinstance(node, nodes.Filter):
        args = "(" + ", ".join(_unparse(a) for a in node.args) + ")" if node.args else ""
        base = _unparse(node.node) if node.node is not None else ""
        return "%s | %s%s" % (base, node.name, args)
    if isinstance(node, nodes.Call):
        return "%s(%s)" % (_unparse(node.node), ", ".join(_unparse(a) for a in node.args))
    if isinstance(node, nodes.Concat):
        return " ~ ".join(_unparse(n) for n in node.nodes)
    if isinstance(node, nodes.List):
        return "[" + ", ".join(_unparse(i) for i in node.items) + "]"
    if isinstance(node, nodes.Tuple):
        return "(" + ", ".join(_unparse(i) for i in node.items) + ")"
    if isinstance(node, nodes.Dict):
        return "{" + ", ".join("%s: %s" % (_unparse(p.key), _unparse(p.value)) for p in node.items) + "}"
    if isinstance(node, nodes.CondExpr):
        s = "%s if %s" % (_unparse(node.expr1), _unparse(node.test))
        if node.expr2 is not None:
            s += " else %s" % _unparse(node.expr2)
        return s
    if isinstance(node, nodes.Neg):
        return "-%s" % _unparse(node.node)
    if isinstance(node, nodes.Pos):
        return "+%s" % _unparse(node.node)
    sym = _BINOPS.get(type(node))
    if sym:
        return "%s %s %s" % (_unparse(node.left), sym, _unparse(node.right))
    raise ValueError("unsupported node %s" % type(node).__name__)


def _resolve(expr, varsmap, env):
    tmpl_ast = env.parse("{{ %s }}" % expr)
    root = tmpl_ast.body[0].nodes[0]
    return _unparse(_simplify(root, varsmap))


def main():
    req = json.load(sys.stdin)
    varsmap = req.get("vars") or {}
    exprs = req.get("exprs") or []
    env = jinja2.Environment()
    out = []
    for expr in exprs:
        try:
            out.append(_resolve(expr, varsmap, env))
        except Exception:
            out.append(expr)
    json.dump(out, sys.stdout)


if __name__ == "__main__":
    main()
