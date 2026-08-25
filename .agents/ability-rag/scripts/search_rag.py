#!/usr/bin/env python3
import argparse, json, sqlite3
from pathlib import Path

DEFAULT_DB = Path(__file__).resolve().parents[1] / 'rag-db' / 'rag.sqlite'

def main():
    parser = argparse.ArgumentParser(description='Search the ability RAG SQLite database.')
    parser.add_argument('query', help='FTS query or plain keyword query, e.g. OKR OR 复盘')
    parser.add_argument('--category', choices=['thinking-models','work-methodology','communication-skills'])
    parser.add_argument('--limit', type=int, default=8)
    parser.add_argument('--db', default=str(DEFAULT_DB))
    args = parser.parse_args()
    con = sqlite3.connect(args.db)
    con.row_factory = sqlite3.Row
    query = args.query
    params = [query]
    where = 'chunk_fts match ?'
    if args.category:
        where += ' and category = ?'
        params.append(args.category)
    sql = f"""
        select chunk_id, category, title, summary, text
        from chunk_fts
        where {where}
        limit ?
    """
    params.append(args.limit)
    try:
        rows = con.execute(sql, params).fetchall()
    except sqlite3.OperationalError:
        # Fall back to quoted phrase for plain strings with punctuation.
        params[0] = '"' + query.replace('"', ' ') + '"'
        rows = con.execute(sql, params).fetchall()
    for r in rows:
        print(json.dumps({
            'chunk_id': r['chunk_id'],
            'category': r['category'],
            'title': r['title'],
            'summary': r['summary'],
            'text': r['text'][:1200],
        }, ensure_ascii=False))
    con.close()

if __name__ == '__main__':
    main()
