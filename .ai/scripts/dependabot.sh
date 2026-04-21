#!/bin/bash
gh api \
  -H "Accept: application/vnd.github+json" \
  /repos/digiogithub/pando/dependabot/alerts \
  | yq -y
