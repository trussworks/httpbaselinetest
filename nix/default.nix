# to install use ./nix/update.sh
#
# Use https://ahobson.github.io/nix-package-search
# to find rev for specific package version

let
  pkgs = import <nixpkgs> {};
  inherit (pkgs) buildEnv;
in buildEnv {
  name = "httpbaselinetest-packages";
  paths = [

    (import (builtins.fetchGit {
      # Descriptive name to make the store path easier to identify
      name = "go-1.18.1";
      url = "https://github.com/NixOS/nixpkgs/";
      ref = "refs/heads/nixpkgs-unstable";
      rev = "d08394e7cd5c7431a1e8f53b7f581e74ee909548";
    }) {}).go_1_18

    (import (builtins.fetchGit {
      # Descriptive name to make the store path easier to identify
      name = "pre-commit-2.17.0";
      url = "https://github.com/NixOS/nixpkgs/";
      ref = "refs/heads/nixpkgs-unstable";
      rev = "7945280b9b2e348738cb9d164a0848feaf2c311d";
    }) {}).pre-commit

    (import (builtins.fetchGit {
      # Descriptive name to make the store path easier to identify
      name = "act-0.2.24";
      url = "https://github.com/NixOS/nixpkgs/";
      ref = "refs/heads/nixpkgs-unstable";
      rev = "a1de1fc28b27da87a84a0b4c9128972ac4a48193";
    }) {}).act


  ];

}
