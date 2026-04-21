#!/bin/bash
gh api \
  -H "Accept: application/vnd.github+json" \
  /repos/digiogithub/pando/code-scanning/alerts | yq -y
