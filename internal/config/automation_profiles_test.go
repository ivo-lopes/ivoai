package config

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestAutomationProfileRejectsConcurrentExternalConfigurationEdit(t *testing.T) {
	store := NewStore(testPaths(t.TempDir()))
	if err := store.Save(Default()); err != nil {
		t.Fatal(err)
	}
	var external []byte
	err := store.updateAutomationConfig(func(c *Config) error {
		// An editor outside IVOAI changes policy while a profile is prepared.
		body, err := os.ReadFile(store.Paths.Config)
		if err != nil {
			return err
		}
		external = bytes.Replace(body, []byte("provider_preference = 'auto'"), []byte("provider_preference = 'claude'"), 1)
		if bytes.Equal(body, external) {
			t.Fatal("fixture did not edit provider")
		}
		if err := os.WriteFile(store.Paths.Config, external, 0600); err != nil {
			return err
		}
		profile, _ := FindAutomationProfile("economic")
		profile.Values.apply(&c.Orchestration.Auto)
		return nil
	})
	if err == nil {
		t.Fatal("concurrent edit overwritten by automation profile")
	}
	got, err := os.ReadFile(store.Paths.Config)
	if err != nil || !bytes.Equal(got, external) {
		t.Fatal("external configuration not preserved", err)
	}
}

func TestAutomationProfileRestoreWithoutReceiptDoesNotCreateConfiguration(t *testing.T) {
	store := NewStore(testPaths(t.TempDir()))
	if err := store.RestoreAutomationProfile(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Paths.Config); !os.IsNotExist(err) {
		t.Fatal("empty rollback created configuration", err)
	}
}

func TestAutomationProfileSerializesManagedWrites(t *testing.T) {
	store := NewStore(testPaths(t.TempDir()))
	if err := store.Save(Default()); err != nil {
		t.Fatal(err)
	}
	err := store.updateAutomationConfig(func(c *Config) error {
		other := NewStore(store.Paths)
		external := Default()
		external.Orchestration.Auto.ProviderPreference = "claude"
		if err := other.Save(external); err == nil {
			t.Fatal("concurrent managed write not excluded")
		}
		if err := other.ApplyAutomationProfile("balanced", nil); err == nil {
			t.Fatal("concurrent profile not excluded")
		}
		profile, _ := FindAutomationProfile("economic")
		profile.Values.apply(&c.Orchestration.Auto)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || got.Orchestration.Auto.ProviderPreference != "auto" {
		t.Fatal("unexpected provider change", err)
	}
	if err := store.Save(got); err != nil {
		t.Fatal("lock not released", err)
	}
}

func TestAutomationProfilesPreservePolicyAndValidateRollback(t *testing.T) {
	for _, p := range AutomationProfiles() {
		t.Run(p.ID, func(t *testing.T) {
			store := NewStore(testPaths(t.TempDir()))
			c := Default()
			c.Orchestration.Auto.ProviderPreference = "claude"
			c.Orchestration.Auto.PlanExecution = "approve"
			c.Orchestration.Auto.Profiles.Codex.Strong = AutoProfileConfig{Model: "custom-model", Effort: "high"}
			if err := store.Save(c); err != nil {
				t.Fatal(err)
			}
			before := AutomationValuesOf(c.Orchestration.Auto)
			if err := store.ApplyAutomationProfile(p.ID, &before); err != nil {
				t.Fatal(err)
			}
			got, err := store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if got.Orchestration.Auto.ProviderPreference != "claude" || got.Orchestration.Auto.PlanExecution != "approve" || got.Orchestration.Auto.Profiles != c.Orchestration.Auto.Profiles {
				t.Fatal("profile crossed policy/provider boundary")
			}
			if err := store.ApplyAutomationProfile(p.ID, &before); err == nil && !reflect.DeepEqual(before, p.Values) {
				t.Fatal("stale preview accepted")
			}
			got.Orchestration.Auto.WorkerCap = 7
			if err := store.Save(got); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(EffectiveAutomationProfile(got), "custom") {
				t.Fatal("override hidden")
			}
			if err := store.RestoreAutomationProfile(); err == nil {
				t.Fatal("rollback overwrote later customization")
			}
		})
	}
}

func TestAutomationProfileAtomicSnapshotAndUnknownPreservation(t *testing.T) {
	store := NewStore(testPaths(t.TempDir()))
	if err := store.Save(Default()); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(store.Paths.Config, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("\n[future]\nkeep = 'yes'\n")
	f.Close()
	if err := store.ApplyAutomationProfile("economic", nil); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Orchestration.Auto.AutomationProfile.Previous == nil || got.Orchestration.Auto.AutomationProfile.Applied == nil {
		t.Fatal("atomic rollback point missing")
	}
	info, _ := os.Stat(store.Paths.Config)
	if info.Mode().Perm() != 0600 {
		t.Fatal("private mode lost")
	}
	got.Orchestration.Auto.AutomationProfile.Previous.WorkerCap = 999
	if err := store.Save(got); err == nil {
		t.Fatal("invalid rollback snapshot saved")
	}
	if err := store.RestoreAutomationProfile(); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(store.Paths.Config)
	if !strings.Contains(string(body), "keep = 'yes'") && !strings.Contains(string(body), `keep = "yes"`) {
		t.Fatal("unknown fields lost")
	}
}
