#!/usr/bin/env bash
set -Eeuo pipefail

cd "$(dirname "$0")/.."
image=docker.cnb.cool/oneclickvirt/ndpresponder/ndpresponder
revision=$(git rev-parse HEAD)
subject=$(git log -1 --format=%s)
if [[ "$subject" =~ ^sync:\ public\ source\ snapshot\ ([0-9a-f]{40})$ ]]; then
    revision=${BASH_REMATCH[1]}
fi

# The Dockerfile cross-compiles Go on BUILDPLATFORM; no emulated RUN step is
# needed for the other target architecture. Publish tags only after tests pass.
for arch in amd64 arm64; do
    docker buildx build --platform "linux/$arch" --provenance=false --load \
        --label org.opencontainers.image.source=https://github.com/oneclickvirt/ndpresponder \
        --label "org.opencontainers.image.revision=$revision" \
        -t "$image:$arch" .
    actual=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image:$arch")
    [[ "$actual" == "linux/$arch" ]]
done

docker run --rm "$image:amd64" --help
for arch in amd64 arm64; do
    docker push "$image:$arch"
done

docker manifest create "$image:latest" "$image:amd64" "$image:arm64"
docker manifest push --purge "$image:latest"
docker manifest inspect "$image:latest" > /tmp/ndpresponder-manifest.json
python3 - <<'PY'
import json
with open('/tmp/ndpresponder-manifest.json') as stream:
    manifests = json.load(stream)['manifests']
platforms = {(entry['platform']['os'], entry['platform']['architecture']) for entry in manifests}
assert len(manifests) == 2 and platforms == {('linux', 'amd64'), ('linux', 'arm64')}, platforms
print('CNB latest manifest verified: linux/amd64 and linux/arm64')
PY
docker pull --platform linux/amd64 "$image:latest"
docker run --rm --platform linux/amd64 "$image:latest" --help
