package ability

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestClassifyDomainsMatchesWorkThinkingAndCommunication(t *testing.T) {
	domains := ClassifyDomains("我要做一个项目复盘，并判断下一步怎么和老板沟通")

	if !containsDomain(domains, DomainWorkMethodology) {
		t.Fatalf("domains = %#v, want work methodology", domains)
	}
	if !containsDomain(domains, DomainThinkingModels) {
		t.Fatalf("domains = %#v, want thinking models", domains)
	}
	if !containsDomain(domains, DomainCommunicationSkills) {
		t.Fatalf("domains = %#v, want communication skills", domains)
	}
}

func TestBuilderReturnsPrivateContextWithoutSourceTitles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rag.sqlite")
	createAbilityTestDB(t, dbPath)

	builder := NewBuilder(Options{DBPath: dbPath, MaxChunks: 3})
	contextText, err := builder.BuildContext(context.Background(), "绩效面谈怎么沟通")
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if contextText == "" {
		t.Fatal("BuildContext returned empty context")
	}
	if strings.Contains(contextText, "敏感资料标题") {
		t.Fatalf("context leaked source title: %q", contextText)
	}
	if strings.Contains(contextText, "source_path") || strings.Contains(contextText, ".pdf") {
		t.Fatalf("context leaked source metadata: %q", contextText)
	}
	if strings.Contains(contextText, "communication-skills") {
		t.Fatalf("context leaked internal category: %q", contextText)
	}
	if !strings.Contains(contextText, "不要输出资料标题") {
		t.Fatalf("context is missing no-citation instruction: %q", contextText)
	}
	if !strings.Contains(contextText, "默认使用简体中文") {
		t.Fatalf("context is missing Simplified Chinese instruction: %q", contextText)
	}
	if !strings.Contains(contextText, "关键名词可以保留英文") {
		t.Fatalf("context is missing English term instruction: %q", contextText)
	}
	if !strings.Contains(contextText, "不要用“当然”") {
		t.Fatalf("context is missing humanizer instruction: %q", contextText)
	}
	if !strings.Contains(contextText, "先确认事实") {
		t.Fatalf("context is missing retrieved guidance: %q", contextText)
	}
}

func TestBuilderMissingDBReturnsEmptyContext(t *testing.T) {
	builder := NewBuilder(Options{DBPath: filepath.Join(t.TempDir(), "missing.sqlite")})
	contextText, err := builder.BuildContext(context.Background(), "怎么复盘")
	if err != nil {
		t.Fatalf("BuildContext returned error: %v", err)
	}
	if contextText != "" {
		t.Fatalf("contextText = %q, want empty", contextText)
	}
}

func createAbilityTestDB(t *testing.T, path string) {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		create virtual table chunk_fts using fts5(
			chunk_id unindexed,
			category unindexed,
			title,
			tags,
			text,
			summary,
			tokenize='unicode61'
		);
		insert into chunk_fts(chunk_id, category, title, tags, text, summary)
		values
			('c1', 'communication-skills', '敏感资料标题', '绩效 沟通',
			 '绩效面谈要先确认事实，再表达影响，最后讨论下一步行动。',
			 '先确认事实，再共同确定行动。'),
			('c2', 'work-methodology', '另一个标题', '复盘',
			 '复盘要先对齐目标和结果，再分析偏差原因。',
			 '目标、结果、原因、行动。');
	`)
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
}

func containsDomain(domains []Domain, target Domain) bool {
	for _, domain := range domains {
		if domain == target {
			return true
		}
	}
	return false
}
