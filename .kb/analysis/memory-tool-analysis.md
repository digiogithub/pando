# Analysis: Previous Analysis of Memory Tools

Reusing the KB with the `memory` tag is a good foundation, but the key is that the agent should not have to "decide to search." Some ideas, ranked by impact:

**1. Automatic injection into the prompt (most important)**
The most transparent memory is the one the agent doesn't query: before each turn, retrieve the N most relevant memories (hybrid BM25+vector against the user message) plus those marked as `pinned`, and inject them into the system prompt in a `<memories>` block. The agent "knows" them without making tool calls. Define a fixed token budget (e.g., 800-1500) and fill by score.

**2. Dedicated tools with simple semantics, not generic ones**
Instead of exposing `kb_search(query, filter=...)`, expose three minimal tools:
- `remember(content, key?, ttl?)` — upsert
- `recall(query)` — optional if you already inject automatically
- `forget(key | query)`

Internally they write to the KB with the `memory` tag, but the agent sees a 1-2 parameter API. LLMs use tools with intent-describing names much better than generic tools with filters.

**3. Key-value with upsert, not documents**
Fast memories are atomic facts ("the user prefers Go", "the deploy is in GCP europe-west1"). Add a `key` field in the frontmatter (e.g., `user.preferred_lang`) and upsert by key: if the agent remembers something new about the same topic, overwrite instead of accumulating contradictory duplicates. One markdown file per memory, frontmatter with `key`, `created`, `updated`, `ttl`, `importance`, `source` (which session generated it).

**4. Scopes/namespaces**
Prefix the keys or use a `scope` field: `user/` (stable preferences), `project/` (active project context), `session/` (ephemeral, short TTL). This way you can always inject `user/*` completely (usually few) and retrieve `project/*` by relevance.

**5. Different ranking from the KB**
For memory, the score should combine similarity + recency (`updated`) + access frequency (a `hits` counter that increments on each recall/injection). A memory used 20 times is worth more than a semantically similar one that was never accessed. This also gives you free signal for expiration: `hits=0` and `updated > 90 days` → candidate for purging.

**6. Capture at write-time, not just on demand**
Connecting with your session-learning pipeline: beyond the explicit `remember` tool, a post-session extractor can detect memorable facts (declared preferences, decisions, user corrections) and write them as memories. The agent "remembers" things it never consciously decided to save — that's what gives the feeling of real memory.

**7. Minimal instruction in the system prompt**
Something like: "You have memories in `<memories>`. If the user tells you something that should persist between sessions (preferences, environment data, decisions), use `remember`." Without this, models systematically underuse the write tool.

If I had to prioritize: start with (1) + (2) + (3). Automatic injection eliminates 80% of the "KB query complexity" problem, and upsert by key prevents memory from degrading into contradictory noise, which is the typical failure mode of these systems.
