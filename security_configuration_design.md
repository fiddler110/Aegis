# Security Configuration in Aegis: Design and Implementation

## Overview

Aegis provides two primary mechanisms for configuring security scanners:
1. **Interactive TUI Wizard**: `/security-config` command that allows users to configure individual scanner settings  
2. **Configuration CLI**: `aegis security config` command for viewing current configuration (read-only)

The system supports both global and project-level configurations, with the interactive wizard being the recommended approach for modifying settings.

## Configuration Architecture

### Security Scanner Components
Aegis includes 12 built-in security scanners:
- opengrep (SAST engine)
- gosec/bandit/brakeman/njsscan (language-specific engines)
- trivy, gitleaks, kubescape, hadolint, osv-scanner, grype, dockle

Each scanner has three key configuration parameters:
1. **Enabled**: Whether the tool is active for scanning
2. **Method**: Where to run the tool ("host", "container", or "auto") 
3. **Install Policy**: How/when installation prompts are shown ("prompt", "always", or "never")

### Key Functions and Files

**Core Security Logic**
- `internal/security/security.go`: Core scanning logic and report generation
- `internal/security/method.go`: Resolves between host/container execution methods  
- `internal/security/install.go`: Handles guided installations with OS-specific commands

**Interactive Configuration (TUI)**
- `internal/tui/securityconfig.go`: `/security-config` interactive dialog UI implementation
- `internal/tui/slash.go`: Command registration for `/security-config`
- `internal/cli/scan.go`: Commands that utilize security configuration  

## How Misconfiguration Detection (Wizards) Work

### 1. Tool Resolution Process

Aegis uses a two-phase system to determine how scanners should run:

**Phase 1: Configuration Review**
- Checks the tool's enabled status in config
- Looks at configured method ("auto", "host", or "container")
- Applies install policy for auto-install behavior  

**Phase 2: Availability Testing**
- **Host Method**: Checks if required binary is available on PATH
- **Container Method**: Tests container runtime availability (Docker, Podman, etc.)

The resolution returns one of three methods:
3. `security.MethodNone`: Tool cannot be run at all ("opt-in tool not enabled", "host binary missing", "container runtime unavailable")
2. `security.MethodContainer`: Tool will run in a container
1. `security.MethodHost`: Tool will run via native binary

### 2. TUI Configuration Wizard Implementation

The `/security-config` command opens an interactive dialog that:
- **Shows current availability**: Displays whether each scanner is available ("on PATH", "container (Docker)", or reason for unavailability)
- **Allows configuration changes**: Per-scanner controls to enable/disable, change method, set install policy
- **Provides guided installations**: Shows exact commands when installing tools via OS package managers

### 3. Wizard Behavior During Scan Execution

When a scan is executed (`/scan` command), the process:
1. Uses the currently configured security settings (from both global and project config)
2. Resolves each enabled scanner to its appropriate execution method 
3. Reports failures for any tools that cannot be run with current configuration
4. Includes suppression information from `.aegis/security-baseline.yaml`

## Key Features of Misconfiguration Handling

### 1. Clear Status Indicators

The system provides explicit status indicators:
- **On PATH**: Tool binary available locally 
- **Container (runtime type)**: Container runtime is available  
- **Unavailable reason**: Detailed explanation when tool cannot run due to missing binaries or permissions

### 2. Intelligent Installation Guidance  

When tools require installation, the wizard shows exactly what command would be run on the user's system:
- OS-specific formulas for Homebrew/Linux package managers
- Clear distinction between optional ("prompt") vs required ("always") installations
- Immediate feedback after attempted install showing success/failure

### 3. Prevention of Silent Failures  

The security architecture ensures no silent skips occur:
1. Every resolution attempt returns either a valid method or specific failure reason
2. Disabled tools are not silently skipped, they're marked as "opt-in tool"
3. Misconfigurations (missing dependencies) always show detailed reasons instead of failing silently

## Design Benefits

### 1. Transparency
- Clear indication of why each scanner won't run when a user attempts to enable it
- Complete visibility into current system configuration
- Detailed reasoning for any resolution failures  

### 2. User Empowerment  
- Interactive wizard allows users to easily see and modify security tool configurations 
- No shell/command-line experience required - everything through guided UI
- Guided installation removes barrier of manual software management

### 3. Safety Through Explicitness
- Container fallback requires explicit image configuration (no default containers shipped)
- Installation policy defaults to "prompt" for user confirmation  
- Host execution requires binaries to be present or explicit install decisions 

## Default Configuration Behavior

By design, most scanners are initially disabled ("opt-in") to prevent:
1. Unintended performance impact from running additional tools
2. Confusion caused by seeing irrelevant security findings 
3. Silent failures that occur with missing tool requirements  

The configuration system encourages users to explicitly enable tools they want and understand how to run them.

## Summary

Aegis implements a complete, user-friendly approach to security scanner configuration where:
- Misconfiguration is never silent or mysterious
- Users see exactly why tools can't execute  
- Configuration changes are intuitive through TUI 
- Installation guides handle OS-specific tool management
- Security scanning maintains clear safety boundaries without requiring deep system expertise

The wizard architecture ensures that users only enable and use security tools they have the capability to run, making configuration robust against environmental failures.