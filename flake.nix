{
  description = "Application deployment contract for the homelab";

  inputs = {
    nixpkgs.url = "https://flakehub.com/f/NixOS/nixpkgs/0.2605";
    git-hooks = {
      url = "https://flakehub.com/f/cachix/git-hooks.nix/0.1";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = {
    nixpkgs,
    git-hooks,
    ...
  }: let
    system = "x86_64-linux";
    pkgs = import nixpkgs {inherit system;};
    applicationContract = pkgs.buildGoModule {
      pname = "application-contract";
      version = "2.2.1";
      src = ./.;
      vendorHash = "sha256-QE/EwVzMqUO24ZAl0WBibGx6x0kNo1AUTZtfnQvX50k=";
    };
    preCommitCheck = git-hooks.lib.${system}.run {
      package = pkgs.prek;
      src = ./.;
      hooks = {
        alejandra.enable = true;
        check-added-large-files.enable = true;
        check-merge-conflicts.enable = true;
        end-of-file-fixer.enable = true;
        gofmt.enable = true;
        trim-trailing-whitespace.enable = true;
      };
    };
  in {
    packages.${system}.default = applicationContract;
    checks.${system} = {
      default = applicationContract;
      pre-commit = preCommitCheck;
    };
    apps.${system}.default = {
      type = "app";
      program = "${applicationContract}/bin/application-contract";
    };
    formatter.${system} = pkgs.alejandra;
    devShells.${system}.default = pkgs.mkShell {
      packages = preCommitCheck.enabledPackages ++ [pkgs.go];
      inherit (preCommitCheck) shellHook;
    };
  };
}
