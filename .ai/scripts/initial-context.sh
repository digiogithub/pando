#!/usr/bin/bash

# get path of this script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/../.."

echo "===== System and Date Information ====="
echo "These are useful information for apply in historical context"
# Detect OS and version
OS=$(uname -s)
case $OS in
    Linux)
        if grep -q Microsoft /proc/version; then
            echo "Operating System: Windows WSL"
            echo "Version: $(uname -r)"
        else
            if [ -f /etc/os-release ]; then
                . /etc/os-release
                echo "Operating System: Linux"
                echo "Version: $PRETTY_NAME"
            else
                echo "Operating System: Linux"
                echo "Version: $(uname -r)"
            fi
        fi
        ;;
    Darwin)
        echo "Operating System: macOS"
        echo "Version: $(sw_vers -productVersion)"
        ;;
    *)
        echo "Operating System: $OS"
        echo "Version: $(uname -r)"
        ;;
esac

# Current date and time
echo "Current Date and Time: $(date)"

echo "===== Repository Status ====="
echo "These are pending changes in the repository, in the working directory"
# Detect repo type and status
if [ -d .jj ]; then
    echo "Repository Type: Jujutsu (jj)"
    echo "Modified Files in Current Change:"
    jj st | head -100
elif [ -d .git ]; then
    echo "Repository Type: Git"
    echo "Current Branch: $(git branch --show-current)"
    echo "Modified Files (uncommitted):"
    git diff --name-only | head -100
    git diff --cached --name-only | head -100
else
    echo "No Git or Jujutsu repository detected."
fi

echo "===== Serena Plan Status ====="
echo "Check if threre are any pending tasks in the Serena plan"

# Check for PLAN.md and list tasks
if [ -f docs/plan.md ]; then
    echo "Tasks from docs/plan.md:"
    grep "^- \[\]" docs/plan.md
else
    echo "No docs/plan.md found."
fi

echo "===== Markdown Files ====="
echo "These are useful information for context"
# List .md files in root and .serena/memories


echo "Last Markdown files modified with context of last tasks in root, docs and docs/memories:"
(find . -maxdepth 1 -name "*.md" -not -name "AGENTS.md" -type f -printf '%T@\t%p\n' ; find docs/ -maxdepth 1 -name "*.md" -type f -printf '%T@\t%p\n' ; find .serena/memories -name "*.md" -type f -printf '%T@\t%p\n') | sort -nr | head -6 | cut -f2-