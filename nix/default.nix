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
      name = "go-1.17.2";
      url = "https://github.com/NixOS/nixpkgs/";
      # Using master branch since 1.17.2 hasn't made it to nixpkgs-unstable yet
      ref = "refs/heads/master";
      rev = "db3aa421df73f43c03ad266619e22ce7c5354d92";
    }) {}).go_1_17

    (import (builtins.fetchGit {
      # Descriptive name to make the store path easier to identify
      name = "pre-commit-2.14.0";
      url = "https://github.com/NixOS/nixpkgs/";
      ref = "refs/heads/nixpkgs-unstable";
      rev = "9c3de9dd586506a7694fc9f19d459ad381239e34";
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
