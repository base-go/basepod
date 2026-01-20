#!/bin/bash
set -e

SERVER="root@d.common.al"
REMOTE_DIR="/opt/deployer"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

deploy_frontend() {
    echo -e "${BLUE}Building frontend...${NC}"
    cd web
    bun install
    bun run generate
    cd ..

    echo -e "${BLUE}Syncing frontend to server...${NC}"
    ssh ${SERVER} "rm -rf ${REMOTE_DIR}/web && mkdir -p ${REMOTE_DIR}/web"
    rsync -avz web/.output/public/ ${SERVER}:${REMOTE_DIR}/web/

    echo -e "${GREEN}Frontend deployed (no Go rebuild needed)${NC}"
}

deploy_backend() {
    echo -e "${BLUE}Syncing source to server...${NC}"
    rsync -avz --exclude '.git' --exclude '/deployerd' --exclude '/deployer' \
        --exclude 'web/.output' --exclude 'web/node_modules' \
        ./ ${SERVER}:${REMOTE_DIR}/

    echo -e "${BLUE}Building Go binary on server...${NC}"
    ssh ${SERVER} "cd ${REMOTE_DIR} && go build -o deployerd ./cmd/deployerd"

    echo -e "${BLUE}Restarting service...${NC}"
    ssh ${SERVER} "systemctl stop deployer && cp ${REMOTE_DIR}/deployerd ${REMOTE_DIR}/bin/deployer && systemctl start deployer"

    echo -e "${GREEN}Backend deployed${NC}"
}

deploy_all() {
    deploy_frontend
    deploy_backend
}

case "${1:-all}" in
    frontend|fe|f)
        deploy_frontend
        ;;
    backend|be|b)
        deploy_backend
        ;;
    all|both)
        deploy_all
        ;;
    *)
        echo "Usage: $0 [frontend|backend|all]"
        echo "  frontend (fe, f) - Deploy frontend only"
        echo "  backend (be, b)  - Deploy backend only (includes frontend embed)"
        echo "  all (both)       - Deploy everything (default)"
        exit 1
        ;;
esac

echo -e "${GREEN}Deploy complete!${NC}"
