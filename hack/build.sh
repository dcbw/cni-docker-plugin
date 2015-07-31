#!/bin/bash

set -e

OSDN_ROOT=$(
  unset CDPATH
  osdn_root=$(dirname "${BASH_SOURCE}")/..
  cd "${osdn_root}"
  pwd
)

OS_OUTPUT="${OSDN_ROOT}/_output/local"
readonly OSDN_GO_PACKAGE=cni-docker-plugin
readonly OSDN_GOPATH="${OSDN_ROOT}/_output/local/go"

setup_env() {
  if [[ -z "$(which go)" ]]; then
    echo "Can't find 'go' in PATH, please fix and retry."
    exit 2
  fi

  local go_pkg_dir="${OSDN_GOPATH}/src/${OSDN_GO_PACKAGE}"
  local go_pkg_basedir=$(dirname "${go_pkg_dir}")
  mkdir -p "${go_pkg_basedir}"
  mkdir -p "${OSDN_GOPATH}/bin"
  rm -f "${go_pkg_dir}"

  # TODO: This symlink should be relative.
  ln -s "${OSDN_ROOT}" "${go_pkg_dir}"

  GOPATH=${OSDN_GOPATH}:${OSDN_ROOT}/Godeps/_workspace
  export GOPATH
  echo $GOPATH
  echo $go_pkg_dir
}

setup_env
go get -d -tags netgo
go install ${OSDN_GO_PACKAGE}

