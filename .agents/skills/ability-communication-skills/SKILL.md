---
name: ability-communication-skills
description: Use when the user needs communication, reporting, presentation, negotiation, performance conversation, feedback, or stakeholder alignment help.
---

# 沟通技巧

Use this skill to turn the user's request into practical, non-generic guidance grounded in the local ability knowledge base. Keep answers concise and action-oriented.

## Boundaries

- Do not load or quote the original Office/PDF files directly.
- Use the RAG index only when the user needs examples, templates, or concrete methods beyond common reasoning.
- Prefer one clear framework over listing many concepts.
- If evidence comes from RAG, cite the retrieved source title in plain text.

## Workflow

1. Identify the user's scenario and desired output.
2. Choose a small set of relevant concepts from this domain.
3. If more detail is needed, search the repository RAG DB from the repo root:

```bash
python3 .agents/ability-rag/scripts/search_rag.py "<关键词>" --category communication-skills --limit 5
```

4. Synthesize the result into steps, checklist, script, table, or message draft depending on the user's request.
5. Remove slogans and empty management jargon; keep only decisions, actions, examples, and tradeoffs.

The RAG source index is expected to cover every original file in this category. Some binary assets may only have metadata entries when readable text cannot be extracted.

## Query Hints

Useful terms: 汇报、表达、演讲、绩效面谈、谈判、沟通.

## Output Style

- For diagnosis: use `现象 -> 原因 -> 方法 -> 下一步`.
- For planning: use `目标 -> 约束 -> 路径 -> 检查点`.
- For templates: provide a directly usable draft, then explain how to adapt it.
