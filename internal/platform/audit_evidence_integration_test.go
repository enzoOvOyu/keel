package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/auditevidence"
)

func TestAuditEvidenceDetectsOwnerChangesWithoutExportingMetadata(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	appendFixture := func(action string) {
		t.Helper()
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err = appendAudit(ctx, tx, "", "", action, "fixture-target", "test", map[string]any{"private_fixture_value": "must-not-be-exported"}); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	appendFixture("test.evidence.first")
	appendFixture("test.evidence.second")
	dir := t.TempDir()
	private, public := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	if _, err := auditevidence.GenerateKeyFiles(private, public); err != nil {
		t.Fatal(err)
	}
	signer, err := auditevidence.LoadSigner(private)
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	trusted, err := auditevidence.LoadPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	var index bytes.Buffer
	envelope, err := s.ExportAuditEvidence(ctx, signer, &index)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(index.Bytes(), []byte("must-not-be-exported")) || bytes.Contains(seal, []byte("private_fixture_value")) {
		t.Fatal("audit metadata leaked to independent evidence")
	}
	if envelope.Payload.Rows != 2 {
		t.Fatalf("unexpected initial row count: %d", envelope.Payload.Rows)
	}
	compare := func() *AuditEvidenceComparison {
		t.Helper()
		out, e := s.CompareAuditEvidence(ctx, bytes.NewReader(index.Bytes()), seal, trusted)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	// The milestone harness supplies one matching built owner CLI for actual
	// process/file tests; ordinary package tests keep the core DB proof above.
	binary := os.Getenv("KEEL_TEST_P1_ADMIN_BIN")
	var cliIndex, cliSeal string
	invoke := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, args...)
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "KEEL_") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		if len(args) > 0 && args[0] != "audit-evidence-verify" {
			cmd.Env = append(cmd.Env, "KEEL_DATABASE_URL="+os.Getenv("KEEL_TEST_DATABASE_URL"))
		}
		return cmd.Output()
	}
	if binary != "" {
		output := filepath.Join(t.TempDir(), "independent-copy")
		if _, e := invoke("audit-evidence-export", "--private-key-file", private, "--output-directory", output); e != nil {
			t.Fatalf("actual evidence export CLI failed: %v", e)
		}
		cliIndex, cliSeal = filepath.Join(output, "audit-index.jsonl"), filepath.Join(output, "seal.json")
		for _, path := range []string{cliIndex, cliSeal} {
			st, e := os.Stat(path)
			if e != nil || st.Mode().Perm() != 0600 {
				t.Fatal("CLI evidence file is not private")
			}
		}
		if _, e := invoke("audit-evidence-verify", "--public-key-file", public, "--index-file", cliIndex, "--seal-file", cliSeal); e != nil {
			t.Fatalf("actual offline verify CLI failed: %v", e)
		}
		if _, e := invoke("audit-evidence-export", "--private-key-file", private, "--output-directory", output); e == nil {
			t.Fatal("CLI reused existing independent evidence directory")
		}
	}
	initial := compare()
	if !initial.Unchanged || initial.Matched != 2 || initial.Additional != 0 {
		t.Fatalf("initial compare: %+v", initial)
	}
	appendFixture("test.evidence.after")
	additional := compare()
	if !additional.Unchanged || additional.Additional != 1 || additional.Matched != 2 {
		t.Fatalf("new audit treated as corruption: %+v", additional)
	}
	// Simulate a DBA deliberately bypassing the append-only trigger. Ordinary
	// UPDATE/DELETE must remain rejected, including for an owner transaction.
	ownerTamper := func(query string) {
		t.Helper()
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if _, e = tx.ExecContext(ctx, "ALTER TABLE audits DISABLE TRIGGER USER"); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.ExecContext(ctx, query); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.ExecContext(ctx, "ALTER TABLE audits ENABLE TRIGGER USER"); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
	}
	ownerTamper(`UPDATE audits SET metadata='{"private_fixture_value":"owner-changed"}' WHERE action='test.evidence.first'`)
	changed := compare()
	if changed.Unchanged || changed.Changed != 1 || changed.Missing != 0 {
		t.Fatalf("owner change not detected: %+v", changed)
	}
	ownerTamper("DELETE FROM audits WHERE action='test.evidence.second'")
	missing := compare()
	if missing.Unchanged || missing.Changed != 1 || missing.Missing != 1 || len(missing.Differences) != 2 {
		t.Fatalf("owner deletion not detected: %+v", missing)
	}
	if binary != "" {
		raw, e := invoke("audit-evidence-compare", "--public-key-file", public, "--index-file", cliIndex, "--seal-file", cliSeal)
		var exit *exec.ExitError
		if !errors.As(e, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("actual changed-audit CLI did not exit 1: %v", e)
		}
		var result AuditEvidenceComparison
		if e = json.Unmarshal(raw, &result); e != nil || result.Unchanged || result.Changed != 1 || result.Missing != 1 {
			t.Fatal("actual CLI concealed archived audit differences")
		}
		t.Log("P1_LIFECYCLE_AUDIT_CLI_OK")
	}
	forged := append([]byte{}, index.Bytes()...)
	forged[10] ^= 1
	if _, err = s.CompareAuditEvidence(ctx, bytes.NewReader(forged), seal, trusted); err == nil {
		t.Fatal("forged evidence returned a database verdict")
	}
	role := "keel_test_audit_evidence_" + RandomID()[:12]
	if _, err = s.DB.ExecContext(ctx, "CREATE ROLE "+role+"; GRANT USAGE ON SCHEMA public TO "+role+"; GRANT SELECT ON audits TO "+role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.DB.ExecContext(context.Background(), "DROP OWNED BY "+role+"; DROP ROLE "+role) })
	parsed, err := url.Parse(os.Getenv("KEEL_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("options", "-c role="+role)
	parsed.RawQuery = query.Encode()
	restricted, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer restricted.Close()
	if _, err = restricted.ExportAuditEvidence(ctx, signer, &bytes.Buffer{}); err == nil {
		t.Fatal("runtime database role exported owner evidence")
	}
	if _, err = restricted.CompareAuditEvidence(ctx, bytes.NewReader(index.Bytes()), seal, trusted); err == nil {
		t.Fatal("runtime database role performed owner evidence comparison")
	}
}
