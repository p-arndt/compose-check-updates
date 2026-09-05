`-image` restricts a run to matching images: `ccu check -image traefik` finds `library/traefik`, `-image 'ghcr.io/immich-app/*'` globs the full name, and filtered-out images cost no registry request
