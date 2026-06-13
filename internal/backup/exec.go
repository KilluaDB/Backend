package backup

import "os/exec"

// commandContext is the exec entry point (overridable in tests).
var commandContext = exec.CommandContext
