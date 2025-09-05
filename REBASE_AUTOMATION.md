# Cert-Manager Operator Rebase Automation

This directory contains automation scripts to streamline the cert-manager-operator rebase process.

## Files

- `rebase_automation.sh` - Main automation script
- `example_rebase.sh` - Example usage script with interactive prompts
- `REBASE_AUTOMATION.md` - This documentation

## Quick Start

1. **Set environment variables:**
   ```bash
   export NEW_CERT_MANAGER_VERSION="1.18.2"  # Target cert-manager version
   export NEW_BUNDLE_VERSION="1.18.0"        # Target bundle version
   ```

2. **Run dry-run first:**
   ```bash
   ./rebase_automation.sh --dry-run
   ```

3. **Execute the rebase:**
   ```bash
   ./rebase_automation.sh
   ```

4. **Or use the interactive example:**
   ```bash
   ./example_rebase.sh
   ```

## Environment Variables

### Required
- `NEW_CERT_MANAGER_VERSION` - The new cert-manager version (e.g., "1.19.0")
- `NEW_BUNDLE_VERSION` - The new bundle version (e.g., "1.19.0")

### Optional (Auto-detected if not provided)
- `OLD_BUNDLE_VERSION` - Current bundle version to replace
- `OLD_CERT_MANAGER_VERSION` - Current cert-manager version to replace

## Script Features

### ✅ What the Script Does

1. **Step 1: Bump Dependencies**
   - Updates `go.mod` with new cert-manager version
   - Updates replace directive for OpenShift fork
   - Runs `go mod tidy && go mod vendor`
   - Creates commit: "Bump deps with upstream cert-manager@vX.X.X"

2. **Step 2: Update Makefile**
   - Updates `BUNDLE_VERSION`
   - Updates `CERT_MANAGER_VERSION`
   - Updates `CHANNELS` with new stable version
   - Runs `make update && make bundle`
   - Creates commit: "Update Makefile: BUNDLE_VERSION, CERT_MANAGER_VERSION, CHANNELS"

3. **Step 3: Update CSV Files**
   - Updates ClusterServiceVersion metadata
   - Updates bundle name, version, replaces (sets to OLD_BUNDLE_VERSION), skipRange
   - Updates bundle.Dockerfile and metadata
   - Runs `make update-bindata`
   - Creates commit: "Update CSV: OLM bundle name, version, replaces, skipRange and skips"

4. **Step 4: Manual Replacements**
   - Intelligently searches files for version references (avoids URLs and comments)
   - Updates container image tags, app.kubernetes.io/version labels
   - Updates CSV descriptions and Dockerfile RELEASE_BRANCH
   - Protects against URL corruption in comments
   - Runs `make manifests bundle` to update generated files
   - Creates commit: "More manual replacements"

### 🛡️ Safety Features

- **Dry-run mode** - See what would be changed without making modifications
- **Auto-backup** - Creates backup branch before making changes
- **Prerequisites check** - Validates required tools and environment
- **Auto-detection** - Automatically detects current versions from files
- **Error handling** - Stops on any error with clear error messages
- **Selective execution** - Run individual steps with `--step N`

## Usage Examples

### Basic Usage
```bash
NEW_CERT_MANAGER_VERSION=1.19.0 NEW_BUNDLE_VERSION=1.19.0 ./rebase_automation.sh
```

### Dry Run
```bash
NEW_CERT_MANAGER_VERSION=1.19.0 NEW_BUNDLE_VERSION=1.19.0 ./rebase_automation.sh --dry-run
```

### Run Specific Step
```bash
NEW_CERT_MANAGER_VERSION=1.19.0 NEW_BUNDLE_VERSION=1.19.0 ./rebase_automation.sh --step 2
```

### Skip Git Commits (for testing)
```bash
NEW_CERT_MANAGER_VERSION=1.19.0 NEW_BUNDLE_VERSION=1.19.0 ./rebase_automation.sh --skip-commit
```

## Command Line Options

- `-h, --help` - Show help message
- `-d, --dry-run` - Show what would be done without making changes
- `-s, --step STEP` - Run only specific step (1-4)
- `--skip-commit` - Skip git commits (useful for testing)

## Troubleshooting

### Common Issues

1. **"Not in a git repository"**
   - Ensure you're running the script from the cert-manager-operator repository root

2. **"NEW_CERT_MANAGER_VERSION is not set"**
   - Set the required environment variables before running

3. **"Failed to auto-detect current versions"**
   - Manually set `OLD_BUNDLE_VERSION` and `OLD_CERT_MANAGER_VERSION`

4. **Make commands fail**
   - Ensure all build dependencies are installed
   - Check that you have proper permissions

### Recovery

If something goes wrong:

1. **Find your backup branch:**
   ```bash
   git branch | grep backup-
   ```

2. **Reset to backup:**
   ```bash
   git reset --hard backup-YYYYMMDD-HHMMSS
   ```

3. **Clean up if needed:**
   ```bash
   git clean -fd
   ```

## Validation Steps

After running the automation, validate the changes:

1. **Check git log:**
   ```bash
   git log --oneline -4
   ```

2. **Review changes:**
   ```bash
   git diff HEAD~4
   ```

3. **Test build:**
   ```bash
   make build
   ```

4. **Test bundle generation:**
   ```bash
   make bundle
   ```

5. **Run tests:**
   ```bash
   make test
   ```

## Integration with CI/CD

The script can be integrated into CI/CD pipelines:

```yaml
# Example GitHub Actions usage
- name: Rebase cert-manager-operator
  env:
    NEW_CERT_MANAGER_VERSION: "1.19.0"
    NEW_BUNDLE_VERSION: "1.19.0"
  run: |
    ./rebase_automation.sh --skip-commit
    # Add additional validation steps
```

## Contributing

When modifying the automation script:

1. Test with `--dry-run` first
2. Test individual steps with `--step N`
3. Validate on a test repository
4. Update this documentation

## Version History

- v1.0 - Initial automation script with all 4 rebase steps
- Support for environment variables and auto-detection
- Dry-run mode and safety features 