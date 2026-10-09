#!/bin/sh
# 빌드 버전. 커밋되지 않은 변경이 있으면 시각을 붙여 겹치지 않게 한다 (Agent 자동 업데이트 판단용).
v=$(git describe --tags --always --dirty 2>/dev/null || echo 0.0.1-dev)
case "$v" in
    *-dirty) echo "$v-$(date -u +%Y%m%d%H%M%S)" ;;
    *) echo "$v" ;;
esac
