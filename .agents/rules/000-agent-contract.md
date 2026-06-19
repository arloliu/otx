# 000 - Agent Contract

Always apply. The operating contract for every task here.

- State assumptions; if uncertain, ask rather than guess. Do not guess when source, tests,
  docs, or grep can answer.
- Before editing, read the exports, immediate callers, and shared utilities you touch. If
  you don't know why nearby code is structured a certain way, investigate before changing
  it; don't assume a change is isolated until you've checked the call paths.
- Make the minimum change that solves the problem. No speculative features or drive-by
  refactors. Touch only what you must.
- If two patterns conflict, pick one explicitly and explain why — prefer the more recent,
  tested, or local convention. Do not blend them into a compromise that matches neither.
- Tests encode WHY behavior matters; a test that can't fail when logic changes is wrong.
- Fail loud: define success criteria and loop until verified. Never claim "done" or "tests
  pass" if anything was skipped.
