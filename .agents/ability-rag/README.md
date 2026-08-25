# 机器人能力资料库

本目录由 `重点能力资料` 生成，原始 Office/PDF 文件没有复制进 skill。RAG 索引覆盖每个来源文件；正文无法提取的字体、图片或空表格也会保留元数据记录。

## 内容

- `rag-db/rag.sqlite`: SQLite FTS5 全文检索库。
- `rag-db/index.jsonl`: 文件级索引和摘要。
- `rag-db/sources.jsonl`: 来源文件元数据。
- `rag-db/chunks.jsonl`: 去重后的知识片段。
- `scripts/search_rag.py`: 本地检索脚本。

## 检索示例

```bash
python3 .agents/ability-rag/scripts/search_rag.py "OKR" --category work-methodology --limit 5
python3 .agents/ability-rag/scripts/search_rag.py "绩效面谈 沟通" --category communication-skills --limit 5
python3 .agents/ability-rag/scripts/search_rag.py "SWOT" --category thinking-models --limit 5
```

## 统计

- 来源文件：244
- 去重后知识片段：2817
- 正文提取失败或仅元数据索引：13
- 检索后端：fts5
