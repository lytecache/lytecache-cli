package service

import "testing"

func TestBuildConfigDarwinIsAlwaysUserLevel(t *testing.T) {
	cfg := buildConfigForGOOS([]string{"ui", "--as-service"}, false, "darwin")
	if cfg.Option["UserService"] != true {
		t.Error("darwin must always be a user-level LaunchAgent -- no --system equivalent exists for macOS")
	}
	if cfg.Option["KeepAlive"] != true {
		t.Error("expected KeepAlive=true (restart on crash)")
	}
	if cfg.Option["RunAtLoad"] != true {
		t.Error("expected RunAtLoad=true (start at login)")
	}

	// systemWide is meaningless on darwin -- must not change anything.
	cfgSystem := buildConfigForGOOS([]string{"ui", "--as-service"}, true, "darwin")
	if cfgSystem.Option["UserService"] != true {
		t.Error("darwin's UserService must stay true even when systemWide=true is passed")
	}
}

func TestBuildConfigLinuxDefaultsToUserUnit(t *testing.T) {
	cfg := buildConfigForGOOS(nil, false, "linux")
	if cfg.Option["UserService"] != true {
		t.Error("expected a user-level systemd unit by default (no root required)")
	}
	if cfg.Option["Restart"] != "on-failure" {
		t.Errorf("Restart = %v, want on-failure", cfg.Option["Restart"])
	}
}

func TestBuildConfigLinuxSystemFlagInstallsSystemUnit(t *testing.T) {
	cfg := buildConfigForGOOS(nil, true, "linux")
	if cfg.Option["UserService"] != false {
		t.Error("--system should produce UserService=false (a system-wide unit)")
	}
}

func TestBuildConfigWindowsHasNoUnixOnlyOptions(t *testing.T) {
	cfg := buildConfigForGOOS(nil, false, "windows")
	if _, ok := cfg.Option["UserService"]; ok {
		t.Error("Windows has no user-level SCM concept -- UserService should not be set")
	}
	if _, ok := cfg.Option["Restart"]; ok {
		t.Error("Restart is a systemd-only option -- should not be set for windows")
	}
}

func TestBuildConfigPersistsArgumentsAndIdentity(t *testing.T) {
	args := []string{"ui", "--port", "7070", "--db", "svc=/tmp/svc.db", "--as-service"}
	cfg := buildConfigForGOOS(args, false, "linux")
	if cfg.Name != Name {
		t.Errorf("Name = %q, want %q", cfg.Name, Name)
	}
	if len(cfg.Arguments) != len(args) {
		t.Fatalf("Arguments = %v, want %v", cfg.Arguments, args)
	}
	for i, a := range args {
		if cfg.Arguments[i] != a {
			t.Errorf("Arguments[%d] = %q, want %q", i, cfg.Arguments[i], a)
		}
	}
}
