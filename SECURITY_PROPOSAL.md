# Selective Security Policy Proposal

## Problem Statement

Current security policy is too restrictive - agents require approval for many safe operations like:
- Basic file reads (`cat`, `ls`, `find`)
- Safe docker commands (`docker ps`, `docker logs`)  
- Simple writes to project directories
- Common development tools

This creates unnecessary friction and slows down agent productivity.

## Proposed Solution: Tiered Security Model

### Security Tiers

1. **ALLOW** - Safe operations that run without approval
2. **LOG** - Moderate risk operations that run but are logged  
3. **HITL** - High risk operations requiring human approval
4. **BLOCK** - Dangerous operations that are always blocked

### Implementation Strategy

#### 1. New Config Structure

```yaml
security:
  policy_mode: selective # strict, selective, permissive
  
  safe_operations:
    # These run without any approval
    bash_patterns:
      - "^ls\\b"
      - "^cat\\b" 
      - "^grep\\b"
      - "^find\\b"
      - "^pwd$"
      - "^echo\\b"
      - "^which\\b"
      - "^git log"
      - "^git status" 
      - "^git diff"
      - "^npm list"
      - "^yarn list"
    tools:
      - read
    docker_commands:
      - ps
      - logs  
      - images
      - inspect
      - stats
      - top
      
  logged_operations:
    # These run but are logged for audit
    bash_patterns:
      - "^git checkout"
      - "^git branch"
      - "^npm install"
      - "^yarn install"
      - "^mkdir"
      - "^touch"
    tools:
      - write  # to allowed paths
      - edit   # to allowed paths
    docker_commands:
      - build
      - run    # with restrictions
      - exec
      
  hitl_required:
    # These require human approval  
    bash_patterns:
      - "\\bsudo\\b"
      - "\\bchmod\\b"
      - "\\brm\\b.*-r"
      - "\\bsystemctl\\b"
    tools:
      - bash   # for patterns not in safe/logged
    docker_commands:
      - rm
      - stop
      - restart
      - compose
      
  always_blocked:
    # These are never allowed
    bash_patterns:
      - "rm -rf /"
      - "mkfs"
      - "> /dev/sd" 
      - "dd if="
      - ":(){ :|:& };:"
    volume_mounts:
      - "/etc"
      - "/var" 
      - "/root"
      - "/proc"
      - "/sys"
      - "/dev"
```

#### 2. Smart Pattern Matching

- Use regex patterns for flexible matching
- Context-aware validation (e.g., `rm` in project dir vs system dir)
- Parameter analysis (e.g., docker run with safe vs dangerous flags)

#### 3. Progressive Permissions

- Start with safe operations allowed
- Learn from user approvals to expand safe patterns
- Audit trail for all operations

#### 4. Tool-Specific Logic

- **bash**: Pattern-based classification
- **write/edit**: Path-based + content analysis  
- **read**: Generally safe unless accessing sensitive files
- **docker**: Command + parameter analysis

### Benefits

1. **Improved Productivity**: 80% of common operations run without approval
2. **Maintained Security**: High-risk operations still gated
3. **Better UX**: Less approval fatigue  
4. **Audit Trail**: All operations logged for compliance
5. **Configurable**: Teams can adjust based on risk tolerance

### Rollout Plan

1. **Phase 1**: Implement tiered config structure
2. **Phase 2**: Update TypeScript extensions with new logic
3. **Phase 3**: Add audit logging and metrics  
4. **Phase 4**: Machine learning for pattern refinement

## Implementation Files to Change

### Go Layer
- `internal/config/config.go` - New SecurityConfig structure
- `internal/security/policy.go` - Pattern matching logic
- `internal/security/tiers.go` - NEW: Tier classification logic
- `configs/agentloop.yaml` - Updated config example

### TypeScript Layer  
- `extensions/security-policy.ts` - Tiered decision logic
- `extensions/docker-guard.ts` - Safe vs risky command classification
- `extensions/audit-logger.ts` - NEW: Operation logging

### Tests
- `internal/security/tiers_test.go` - NEW: Test tier classifications
- Update existing security tests

## Example Behavior Changes

### Before (Current)
```
Agent: I want to check what files are here
Tool: bash("ls -la")
System: 🛑 HITL REQUIRED - bash command needs approval
User: (clicks approve) ✅ 
Result: total 64, drwxr-xr-x...
```

### After (Proposed)
```
Agent: I want to check what files are here  
Tool: bash("ls -la")  
System: ✅ SAFE OPERATION - executing
Result: total 64, drwxr-xr-x...
```

### Still Protected
```
Agent: I need to remove this directory
Tool: bash("sudo rm -rf /var/log")
System: 🛑 HITL REQUIRED - destructive command with system path
User: (clicks deny) ❌
Result: Operation blocked by user
```