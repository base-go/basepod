# Deployer Fix Plan

## Current Status

**Version on server:** 0.1.28 (but missing recent fixes)
**Version in code:** 0.1.29

## Issues to Fix

### Issue 1: Proxy Not Forwarding 302 Redirects with Cookies
**File:** `internal/api/api.go` line ~1751
**Problem:** `http.Client{}` follows redirects by default, losing Set-Cookie headers
**Impact:** code-server login returns 401 (password works but cookie not set)

**Fix:**
```go
client := &http.Client{
    Timeout: 60 * time.Second,
    CheckRedirect: func(req *http.Request, via []*http.Request) error {
        return http.ErrUseLastResponse
    },
}
```
**Status:** FIXED in code, not deployed

---

### Issue 2: Template Command Not Passed to Container
**File:** `internal/api/api.go` line ~1299
**Problem:** `deployFromTemplate()` doesn't pass `tmpl.Command` to CreateContainer
**Impact:** code-server binds to 127.0.0.1 instead of 0.0.0.0

**Fix:**
```go
containerID, err := s.podman.CreateContainer(ctx, podman.CreateContainerOpts{
    Name:    "deployer-" + a.Name,
    Image:   image,
    Env:     a.Env,
    Command: tmpl.Command,  // ADD THIS
    ...
})
```
**Status:** FIXED in code, not deployed

---

### Issue 3: Rootful Podman Socket Detection
**File:** `internal/config/config.go` line ~232
**Problem:** On rootful Linux, wasn't checking `/run/podman/podman.sock`
**Impact:** Container count shows 0

**Fix:**
```go
case "linux":
    if os.Getuid() == 0 {
        rootfulSocket := "/run/podman/podman.sock"
        if _, err := os.Stat(rootfulSocket); err == nil {
            return rootfulSocket
        }
    }
    // ... rest of rootless detection
```
**Status:** FIXED in code, not deployed

---

### Issue 4: Cookie SameSite Too Strict
**File:** `internal/api/api.go` line ~193
**Problem:** SameSite=Strict breaks same-site navigation through proxy
**Impact:** Login cookie not sent on redirects

**Fix:** Changed to `SameSiteLaxMode`
**Status:** FIXED in code, not deployed

---

## Branch Strategy

1. **Create fix branch:** `fix/proxy-and-templates`
2. **Verify all fixes are in code** (they are)
3. **Build on server** (CGO required for SQLite)
4. **Test each fix via SSH**
5. **If all working:** Merge to main, tag release v0.1.29
6. **Create release on base-go/basepod**

---

## Testing Plan

### Test 1: Proxy Cookie Forwarding
```bash
# Direct to container (should work)
ssh root@common.al "curl -s -X POST http://localhost:31281/login -d 'password=changeme' -v 2>&1 | grep -i set-cookie"

# Through deployer proxy (should now also work)
ssh root@common.al "curl -s -X POST -H 'Host: code.common.al' http://localhost:3000/login -d 'password=changeme' -v 2>&1 | grep -i set-cookie"
```
**Expected:** Both should show `Set-Cookie: code-server-session=...`

### Test 2: Container Count
```bash
ssh root@common.al "curl -s localhost:3000/api/system/info" | jq '.containers'
```
**Expected:** Should show actual container count (not 0)

### Test 3: Template Command
```bash
# Deploy new code-server from template
# Check container command
ssh root@common.al "podman inspect deployer-code --format '{{.Config.Cmd}}'"
```
**Expected:** Should show `[--bind-addr 0.0.0.0:8080]`

### Test 4: Full Login Flow
```bash
# Open in browser: https://code.common.al
# Enter password: changeme
# Should redirect to VS Code editor (not 401)
```

---

## Deployment Steps

### Step 1: Create branch and verify fixes
```bash
git checkout -b fix/proxy-and-templates
git status  # Should show fixes already committed
```

### Step 2: Build on server
```bash
# Copy source to server (private repo workaround)
rsync -avz --exclude='.git' --exclude='node_modules' --exclude='web/.output' \
  /Users/flakerimismani/Base/deployer/ root@common.al:/tmp/deployer-src/

# Build on server
ssh root@common.al "cd /tmp/deployer-src && CGO_ENABLED=1 go build -o deployer ./cmd/deployerd"
```

### Step 3: Deploy and restart
```bash
ssh root@common.al "cp /tmp/deployer-src/deployer /opt/deployer/bin/deployer && systemctl restart deployer"
```

### Step 4: Verify version
```bash
ssh root@common.al "/opt/deployer/bin/deployer version"
# Expected: deployerd version 0.1.29
```

### Step 5: Run tests
(See Testing Plan above)

### Step 6: If all tests pass
```bash
git checkout main
git merge fix/proxy-and-templates
git tag v0.1.29
git push origin main --tags
```

### Step 7: Create release on base-go/basepod
- Build binaries for all platforms
- Upload to GitHub release v0.1.29

---

## Rollback Plan

If issues occur:
```bash
# Restore previous binary (if backed up)
ssh root@common.al "cp /opt/deployer/bin/deployer.bak /opt/deployer/bin/deployer && systemctl restart deployer"

# Or rebuild from previous version
git checkout v0.1.27
# rebuild and deploy
```

---

## Files Changed

| File | Changes |
|------|---------|
| `cmd/deployerd/main.go` | Version bump 0.1.27 -> 0.1.29 |
| `internal/api/api.go` | Proxy CheckRedirect, Cookie SameSite, Template Command |
| `internal/config/config.go` | Rootful socket detection |
| `internal/templates/templates.go` | Command field for code-server |
