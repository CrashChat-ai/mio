# The Astro/Starlight content collection loads ../docs (the repo's docs/ dir),
# which lives OUTSIDE this build context — so the site is pre-built on the host
# (`npm run build` -> ./dist) and this image just serves the static output with
# nginx on :3000 (the crashchat Service contract is port 80 -> targetPort 3000).
# Build for the prod k3s node arch: docker buildx build --platform linux/amd64 ...
FROM nginx:alpine
COPY dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 3000
