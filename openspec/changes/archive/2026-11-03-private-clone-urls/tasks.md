# Tasks

## 1. Credentials

- [x] 1.1 Config `git_credentials` (host, optional username, `token_env`), `git_ssh_key`,
      `git_known_hosts`; refuse empty token variables and URLs with embedded passwords; verify with
      config tests for each scenario.
- [x] 1.2 Non-interactive git for every network call (prompts off, ssh BatchMode), credentials passed
      as a URL-scoped header through the environment, HTTPS only, SSH key support; verify the
      environment built for HTTPS, plain HTTP, ssh and unknown hosts.
- [x] 1.3 Failure messages: read git's stderr, scrub known tokens, map to plain hints; verify against
      a real `git http-backend` server behind basic auth over TLS: success with the token, immediate
      failure with none, failure with a wrong token, no token anywhere in the reported reason, no
      credential sent over plain HTTP.

## 2. Packaging and docs

- [x] 2.1 Dockerfile gets `openssh-client`; compose reads an optional env file for secrets.
- [x] 2.2 README: how to declare a private repo (GitHub, GitLab, SSH), what is and is not protected.

## Workflow follow-up

- Archive once reviewed.
