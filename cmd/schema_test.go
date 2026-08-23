package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestSchemaCommandRegistered(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"schema"})
	if err != nil {
		t.Fatalf(`rootCmd.Find("schema") error = %v`, err)
	}
	if found.Use != "schema" {
		t.Errorf(`Find("schema").Use = %q, want "schema"`, found.Use)
	}
}

func TestBuildSchemaIncludesGlobalFlagsAndCommands(t *testing.T) {
	s := buildSchema(rootCmd)

	if s.Name != "pvectl" {
		t.Errorf("buildSchema().Name = %q, want %q", s.Name, "pvectl")
	}

	var sawOutputFlag bool
	for _, f := range s.GlobalFlags {
		if f.Name == "output" {
			sawOutputFlag = true
		}
	}
	if !sawOutputFlag {
		t.Errorf("buildSchema().GlobalFlags = %+v, want it to include the --output flag", s.GlobalFlags)
	}

	var ct *schemaCommand
	for i := range s.Commands {
		if s.Commands[i].Name == "ct" {
			ct = &s.Commands[i]
		}
	}
	if ct == nil {
		t.Fatalf("buildSchema().Commands = %+v, want a \"ct\" entry", s.Commands)
	}

	var sawList bool
	for _, sub := range ct.Commands {
		if sub.Name == "list" {
			sawList = true
			var sawNodeFlag bool
			for _, f := range sub.Flags {
				if f.Name == "node" {
					sawNodeFlag = true
				}
			}
			if !sawNodeFlag {
				t.Errorf(`"ct list".Flags = %+v, want it to include "--node"`, sub.Flags)
			}
		}
	}
	if !sawList {
		t.Errorf(`"ct".Commands = %+v, want a "list" entry`, ct.Commands)
	}
}

func TestValidateOutputFormatRejectsUnknownValue(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()

	outputFormat = "yaml"
	if err := validateOutputFormat(rootCmd, nil); err == nil {
		t.Error("validateOutputFormat() error = nil, want error for --output yaml")
	}
}

func TestValidateOutputFormatAcceptsTableAndJSON(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()

	outputFormat = "table"
	if err := validateOutputFormat(rootCmd, nil); err != nil {
		t.Errorf(`validateOutputFormat() with --output "table" error = %v, want nil`, err)
	}
	if jsonOutput {
		t.Error("jsonOutput = true after --output table, want false")
	}

	outputFormat = "json"
	if err := validateOutputFormat(rootCmd, nil); err != nil {
		t.Errorf(`validateOutputFormat() with --output "json" error = %v, want nil`, err)
	}
	if !jsonOutput {
		t.Error("jsonOutput = false after --output json, want true")
	}
}

func TestValidateOutputFormatRejectsEmpty(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()

	outputFormat = ""
	if err := validateOutputFormat(rootCmd, nil); err == nil {
		t.Error(`validateOutputFormat() error = nil, want error for --output ""`)
	}
}

func TestSchemaSubcommandsSkipsHelp(t *testing.T) {
	for _, c := range schemaSubcommands(rootCmd) {
		if c.Name == "help" {
			t.Errorf("schemaSubcommands(rootCmd) included cobra's auto-generated \"help\" command, want it skipped")
		}
	}
}

// findSchemaCommand looks up a dotted path (e.g. "ct.backups.delete")
// within a schema tree built by schemaSubcommands/buildSchema.
func findSchemaCommand(cmds []schemaCommand, path ...string) *schemaCommand {
	for i := range cmds {
		if cmds[i].Name != path[0] {
			continue
		}
		if len(path) == 1 {
			return &cmds[i]
		}
		return findSchemaCommand(cmds[i].Commands, path[1:]...)
	}
	return nil
}

func TestSchemaMutationLevelsMatchKnownCommands(t *testing.T) {
	cmds := schemaSubcommands(rootCmd)

	for _, tc := range []struct {
		path []string
		want string
	}{
		{[]string{"ct", "list"}, mutationSafe},
		{[]string{"ct", "summary"}, mutationSafe},
		{[]string{"ct", "start"}, mutationMutating},
		{[]string{"ct", "create"}, mutationMutating},
		{[]string{"ct", "destroy"}, mutationDestructive},
		{[]string{"ct", "template"}, mutationDestructive},
		{[]string{"ct", "backups", "list"}, mutationSafe},
		{[]string{"ct", "backups", "create"}, mutationMutating},
		{[]string{"ct", "backups", "delete"}, mutationDestructive},
		{[]string{"ct", "backups", "restore"}, mutationDestructive},
		{[]string{"ct", "snapshots", "rollback"}, mutationDestructive},
		{[]string{"qm", "destroy"}, mutationDestructive},
		{[]string{"qm", "snapshots", "delete"}, mutationDestructive},
		// exec/enter hand off to an arbitrary command or an interactive
		// shell — an unbounded blast radius, so they're destructive-tier
		// rather than mutating despite not deleting anything themselves.
		{[]string{"ct", "exec"}, mutationDestructive},
		{[]string{"ct", "enter"}, mutationDestructive},
		{[]string{"qm", "enter"}, mutationDestructive},
		{[]string{"nodes", "reboot"}, mutationDestructive},
	} {
		got := findSchemaCommand(cmds, tc.path...)
		if got == nil {
			t.Errorf("findSchemaCommand(%v) = nil, want a command", tc.path)
			continue
		}
		if got.Mutation != tc.want {
			t.Errorf("%v.Mutation = %q, want %q", tc.path, got.Mutation, tc.want)
		}
	}
}

// cobraGeneratedCommands is the allowlist of runnable commands
// TestEveryPvectlCommandHasAnExplicitMutationAnnotation exempts from
// requiring an explicit annotation: cobra's own auto-generated
// `completion <shell>` commands, which pvectl never defines a literal
// for, so there's nowhere to attach one. They only ever print a local
// shell script, so they're inherently safe.
var cobraGeneratedCommands = map[string]bool{
	"bash": true, "zsh": true, "fish": true, "powershell": true,
}

// TestEveryPvectlCommandHasAnExplicitMutationAnnotation walks the raw
// cobra tree directly — unlike schemaSubcommands, it does not fall back
// to mutationSafe for an unannotated Runnable command, so a pvectl-owned
// command that forgets its annotation fails this test loudly instead of
// silently shipping as "safe" (the most dangerous possible default) in
// `pvectl schema`'s actual output.
func TestEveryPvectlCommandHasAnExplicitMutationAnnotation(t *testing.T) {
	var walk func(cmd *cobra.Command, path string)
	walk = func(cmd *cobra.Command, path string) {
		for _, c := range cmd.Commands() {
			p := path + " " + c.Name()
			if c.Hidden || c.Name() == "help" {
				continue
			}
			if c.Runnable() && c.Annotations[mutationAnnotationKey] == "" && !cobraGeneratedCommands[c.Name()] {
				t.Errorf("command %q is runnable but has no explicit mutation annotation", p)
			}
			walk(c, p)
		}
	}
	walk(rootCmd, "")
}

// TestSchemaEveryLeafCommandIsClassified guards against a newly added
// command shipping without a mutation level: every runnable (leaf)
// command in the tree must report a non-empty Mutation, so `pvectl
// schema` never silently omits risk metadata for a real action.
func TestSchemaEveryLeafCommandIsClassified(t *testing.T) {
	var walk func(cmds []schemaCommand, path string)
	walk = func(cmds []schemaCommand, path string) {
		for _, c := range cmds {
			p := path + " " + c.Name
			if len(c.Commands) == 0 && c.Mutation == "" {
				t.Errorf("leaf command %q has no mutation level", p)
			}
			walk(c.Commands, p)
		}
	}
	walk(schemaSubcommands(rootCmd), "")
}
