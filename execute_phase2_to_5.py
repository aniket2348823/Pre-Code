import os
import shutil
import glob
import re

def move_package_files(src_dir, dest_dir, new_name_prefix=None):
    if not os.path.exists(src_dir):
        print(f"Directory {src_dir} does not exist, skipping.")
        return
    if not os.path.exists(dest_dir):
        os.makedirs(dest_dir)

    dest_pkg = os.path.basename(os.path.normpath(dest_dir))

    for f in os.listdir(src_dir):
        src_file = os.path.join(src_dir, f)
        if os.path.isfile(src_file) and f.endswith('.go'):
            with open(src_file, 'r', encoding='utf-8') as file_in:
                content = file_in.read()
            
            # Change package declaration
            content = re.sub(r'^package\s+\w+', f'package {dest_pkg}', content, count=1, flags=re.MULTILINE)
            
            target_name = f
            if new_name_prefix:
                target_name = f"{new_name_prefix}_{f}"
            
            dest_file = os.path.join(dest_dir, target_name)
            with open(dest_file, 'w', encoding='utf-8') as file_out:
                file_out.write(content)
            
            print(f"Moved {src_file} -> {dest_file}")
    
    # Remove old package directory
    shutil.rmtree(src_dir, ignore_errors=True)
    print(f"Removed directory {src_dir}")

def run_phase2():
    print("--- Phase 2: Package Consolidations ---")
    move_package_files("internal/cachekeys", "internal/cache", "keys")
    move_package_files("internal/cachewarm", "internal/cache", "warm")

    move_package_files("internal/logging", "internal/observability", "logging")
    move_package_files("internal/slogger", "internal/observability", "slogger")
    move_package_files("internal/telemetry", "internal/observability", "telemetry")

    move_package_files("internal/cost", "internal/costintel", "cost")

    move_package_files("internal/retry", "internal/util", "retry")
    move_package_files("internal/timeout", "internal/util", "timeout")
    move_package_files("internal/graceful", "internal/util", "graceful")

    move_package_files("internal/critic", "internal/agent", "critic")
    move_package_files("internal/contextbuilder", "internal/agent", "context")
    move_package_files("internal/knowledge", "internal/agent", "knowledge")
    move_package_files("internal/extraction", "internal/agent", "extraction")

    move_package_files("internal/configdrift", "internal/config", "drift")
    move_package_files("internal/compliance", "internal/config", "compliance")
    move_package_files("internal/featureflags", "internal/config", "featureflags")

def run_phase3():
    print("--- Phase 3: Database & Migration Consolidation ---")
    # Squash migrations into 000001_init_schema.up.sql
    m1_up = "migrations/000001_init_schema.up.sql"
    m2_up = "migrations/000002_add_apikey_rotation.up.sql"
    m3_up = "migrations/000003_add_soft_deletes.up.sql"

    if os.path.exists(m1_up):
        with open(m1_up, 'a', encoding='utf-8') as f1:
            if os.path.exists(m2_up):
                with open(m2_up, 'r', encoding='utf-8') as f2:
                    f1.write("\n\n-- API Key Rotation\n" + f2.read())
                os.remove(m2_up)
                print(f"Squashed {m2_up}")
            if os.path.exists(m3_up):
                with open(m3_up, 'r', encoding='utf-8') as f3:
                    f1.write("\n\n-- Soft Deletes\n" + f3.read())
                os.remove(m3_up)
                print(f"Squashed {m3_up}")

    # Remove down migrations for 2 and 3
    for f in ["migrations/000002_add_apikey_rotation.down.sql", "migrations/000003_add_soft_deletes.down.sql"]:
        if os.path.exists(f):
            os.remove(f)
            print(f"Removed {f}")

    # Consolidate Supabase setup
    sup1 = "configs/supabase-full-setup.sql"
    sup2 = "scripts/supabase-setup.sql"
    dest_sup = "configs/supabase_setup.sql"

    if os.path.exists(sup1):
        shutil.copy(sup1, dest_sup)
        os.remove(sup1)
        print(f"Moved {sup1} -> {dest_sup}")
    if os.path.exists(sup2):
        os.remove(sup2)
        print(f"Removed {sup2}")

def run_phase4():
    print("--- Phase 4: Dockerfile Consolidation ---")
    df_prod = "Dockerfile.prod"
    df = "Dockerfile"

    if os.path.exists(df_prod):
        with open(df_prod, 'r', encoding='utf-8') as f_in:
            prod_content = f_in.read()

        multistage_df = f"""# Multi-stage Dockerfile for VigilAgent API Gateway
# Target 'dev' for local development, target 'prod' for production image

FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/server ./cmd/server

# Production Stage
FROM alpine:3.19 AS prod
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/server /app/server
EXPOSE 8080
USER nobody
ENTRYPOINT ["/app/server"]

# Development Stage
FROM golang:1.22-alpine AS dev
WORKDIR /app
COPY . .
EXPOSE 8080
CMD ["go", "run", "./cmd/server"]
"""
        with open(df, 'w', encoding='utf-8') as f_out:
            f_out.write(multistage_df)
        
        os.remove(df_prod)
        print(f"Merged {df_prod} into multi-stage {df}")

def run_phase5():
    print("--- Phase 5: Workspace Hygiene ---")
    # Delete binary files in root
    for exe in glob.glob("*.exe") + ["api", "mcp"]:
        if os.path.isfile(exe):
            try:
                os.remove(exe)
                print(f"Deleted binary artifact: {exe}")
            except Exception as e:
                print(f"Could not delete {exe}: {e}")

    # Delete coverage dumps
    for out in glob.glob("*.out") + ["config.out", "coverage.out", "grace.out", "llm_cov.out", "rate.out", "resp.out", "telem.out", "tools_cov.out", "build_exit.txt"]:
        if os.path.isfile(out):
            try:
                os.remove(out)
                print(f"Deleted log/coverage artifact: {out}")
            except Exception as e:
                print(f"Could not delete {out}: {e}")

    # Move shell scripts to scripts/deploy and scripts/ci
    os.makedirs("scripts/deploy", exist_ok=True)
    os.makedirs("scripts/ci", exist_ok=True)

    deploy_scripts = ["scripts/backup-db.sh", "scripts/blue-green-deploy.sh", "scripts/monitor-health.sh"]
    ci_scripts = ["scripts/check-backwards-compat.sh", "scripts/detect-breaking-changes.sh", "scripts/openapi-codegen.sh"]

    for s in deploy_scripts:
        if os.path.exists(s):
            shutil.move(s, os.path.join("scripts/deploy", os.path.basename(s)))
            print(f"Moved {s} -> scripts/deploy/")

    for s in ci_scripts:
        if os.path.exists(s):
            shutil.move(s, os.path.join("scripts/ci", os.path.basename(s)))
            print(f"Moved {s} -> scripts/ci/")

    # Remove temporary merge scripts
    temp_scripts = ["fix_db_tests.py", "fix_remaining_tests.py", "merge_go.py", "split_go.py", "execute_phase1.py", "scripts/do_merge.py", "scripts/merge_misc_tests.py", "scripts/merge_router.sh", "scripts/merge_router_tests.py", "scripts/merge_router_v2.sh", "scripts/merge_tests.py"]
    for ts in temp_scripts:
        if os.path.exists(ts):
            os.remove(ts)
            print(f"Deleted temp script: {ts}")

if __name__ == "__main__":
    run_phase2()
    run_phase3()
    run_phase4()
    run_phase5()
