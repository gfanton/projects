{
  description = "A Git-based project management tool with zoxide-like navigation for GitHub-style directory structure";

  # For consumers: Use specific version tags or latest from master
  # Examples:
  #   project.url = "github:gfanton/project";                # Latest from master
  #   project.url = "github:gfanton/project?ref=v1.2.3";     # Specific version tag

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    let
      # Global shared version for all packages and systems
      # This version is automatically updated by the release script
      projectVersion = "0.19.0";
    in
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # Define project package
        projectPackage = pkgs.buildGoModule rec {
          pname = "project";
          version = projectVersion;

          src = ./.;

          # Vendor hash - updated by release script or manually during development
          vendorHash = "sha256-wZZsEXvuLfPDO4/+Rbr4qZxPsDLOrKC/4nKwoubfYV8=";

          # Override build flags to not use vendor mode
          buildFlags = [ "-mod=mod" ];

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];

          # Build from cmd/proj
          subPackages = [ "cmd/proj" ];

          # Test dependencies for integration tests
          nativeBuildInputs = with pkgs; [
            git
            zsh
            # bats  # Temporarily disabled due to nokogiri build issue in nixpkgs-unstable
            expect
          ];

          # Skip tests in Nix build - they require interactive environment
          # Tests can be run separately with `make test` or in development shell
          doCheck = false;

          # Install documentation and examples
          postInstall = ''
            # Create examples directory
            mkdir -p $out/share/doc/project/examples

            # Copy shell.nix for development
            cp ${./shell.nix} $out/share/doc/project/examples/shell.nix
          '';

          meta = with pkgs.lib; {
            description = "A Git-based project management tool with zoxide-like navigation";
            longDescription = ''
              A tool that organizes Git repositories in a GitHub-style directory structure
              and provides fast project navigation similar to zoxide. Features include:
              - Fast fuzzy search and navigation between projects
              - Enhanced zsh completion with menu selection
              - GitHub-style organization (username/project-name)
              - Shell integration with the 'p' command
            '';
            homepage = "https://github.com/gfanton/project";
            license = licenses.mit;
            maintainers = [ "gfanton" ];
            mainProgram = "proj";
          };
        };

        # Define proj-tmux package
        projTmuxPackage = pkgs.buildGoModule rec {
          pname = "proj-tmux";
          version = projectVersion;

          src = ./.;

          # Same vendorHash as main project since they share go.mod
          vendorHash = "sha256-wZZsEXvuLfPDO4/+Rbr4qZxPsDLOrKC/4nKwoubfYV8=";

          # Override build flags to not use vendor mode
          buildFlags = [ "-mod=mod" ];

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];

          # Build from plugins/proj-tmux
          subPackages = [ "plugins/proj-tmux" ];

          meta = with pkgs.lib; {
            description = "Tmux integration for proj";
            homepage = "https://github.com/gfanton/project";
            license = licenses.mit;
            maintainers = [ "gfanton" ];
            mainProgram = "proj-tmux";
          };
        };

        # Define proj-herdr package
        projHerdrPackage = pkgs.buildGoModule rec {
          pname = "proj-herdr";
          version = projectVersion;

          src = ./.;

          # Same vendorHash as main project since they share go.mod
          vendorHash = "sha256-wZZsEXvuLfPDO4/+Rbr4qZxPsDLOrKC/4nKwoubfYV8=";

          # Override build flags to not use vendor mode
          buildFlags = [ "-mod=mod" ];

          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];

          # Build from plugins/proj-herdr
          subPackages = [ "plugins/proj-herdr" ];

          # herdr loads a plugin from a directory holding the manifest, with the
          # binary under bin/, so the manifest ships beside it.
          postInstall = ''
            install -Dm444 plugins/proj-herdr/herdr-plugin.toml -t $out
          '';

          meta = with pkgs.lib; {
            description = "Herdr integration for proj";
            homepage = "https://github.com/gfanton/project";
            license = licenses.mit;
            maintainers = [ "gfanton" ];
            mainProgram = "proj-herdr";
          };
        };

        # Define tmux plugin package
        tmuxProjPackage = pkgs.tmuxPlugins.mkTmuxPlugin rec {
          pluginName = "tmux-proj";
          version = projectVersion;
          rtpFilePath = "proj-tmux.tmux";

          src = ./plugins/proj-tmux/plugin;

          # Dependencies - the plugin needs proj and proj-tmux binaries
          buildInputs = [ projectPackage projTmuxPackage ];

          # Wrap the scripts to have access to the binaries
          nativeBuildInputs = [ pkgs.makeWrapper ];

          postInstall = ''
            # Wrap the main plugin script
            wrapProgram $out/share/tmux-plugins/tmux-proj/proj-tmux.tmux \
              --prefix PATH : ${pkgs.lib.makeBinPath [ projectPackage projTmuxPackage ]}

            # Wrap all scripts in the scripts directory
            for script in $out/share/tmux-plugins/tmux-proj/scripts/*.sh; do
              wrapProgram "$script" \
                --prefix PATH : ${pkgs.lib.makeBinPath [ projectPackage projTmuxPackage ]}
            done
          '';

          meta = with pkgs.lib; {
            description = "A tmux plugin for proj - Git-based project management with session integration";
            homepage = "https://github.com/gfanton/project";
            license = licenses.mit;
            platforms = platforms.unix;
            maintainers = [ "gfanton" ];
          };
        };
      in
      {
        # Packages - no self-references to avoid circular dependencies
        packages = {
          default = projectPackage;
          project = projectPackage;
          proj-tmux = projTmuxPackage;
          proj-herdr = projHerdrPackage;
          tmux-proj = tmuxProjPackage;
        };

        # Development shells
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            # Go toolchain
            go_1_25

            # Shells for testing
            bash
            zsh

            # Testing tools
            # bats  # Temporarily disabled due to nokogiri build issue in nixpkgs-unstable
            expect
            tmux

            # Development tools
            gnumake
            git

            # For shell completion testing
            fzf
          ];

          shellHook = ''
            export PROJECT_TEST_DIR="''${PWD}/test-env"
            export PROJECT_TEST_BIN="''${PWD}/build/proj"

            # Add local scripts to PATH for interactive testing commands
            export PATH="''${PWD}/scripts/bin:''${PATH}"
          '';
        };

        # Dedicated testing development shell for tmux integration
        devShells.testing = pkgs.mkShell {
          buildInputs = with pkgs; [
            # Go toolchain
            go_1_25

            # Testing frameworks and tools
            tmux
            expect
            # bats  # Temporarily disabled due to nokogiri build issue in nixpkgs-unstable

            # Shell environments
            bash
            bashInteractive
            zsh

            # Development and testing utilities
            gnumake
            git
            fzf

            # Additional testing utilities
            procps # for ps, needed by tmux testing
            util-linux # for script, timeout commands
          ];

          shellHook = ''
            if [[ ! -f "./build/proj" ]]; then
              make build >/dev/null 2>&1 || echo "Warning: Failed to build proj" >&2
            fi
            export PROJ_BINARY="''${PWD}/build/proj"

            # Only set up isolated test tmux environment if not already in tmux
            if [[ -z "''${TMUX:-}" ]]; then
              export TEST_TMUX_DIR="$(mktemp -d -t tmux-test-XXXXXX)"
              export TEST_TMUX_SOCKET="''${TEST_TMUX_DIR}/test-socket"
              export TMUX_TMPDIR="''${TEST_TMUX_DIR}"
              export TEST_PROJECT_DIR="''${TEST_TMUX_DIR}/projects"
              mkdir -p "''${TEST_PROJECT_DIR}"

              alias test-tmux='tmux -S "''${TEST_TMUX_SOCKET}"'

              cleanup_test_env() {
                tmux -S "''${TEST_TMUX_SOCKET}" kill-server 2>/dev/null || true
                rm -rf "''${TEST_TMUX_DIR}"
              }
              trap cleanup_test_env EXIT
            fi
          '';
        };

        # Apps for easy running
        apps.default = {
          type = "app";
          program = "${projectPackage}/bin/proj";
        };

        # Checks for CI
        checks = {
          # Simple build test without circular dependencies
          project-build = projectPackage;
          proj-tmux-build = projTmuxPackage;
          tmux-proj-build = tmuxProjPackage;

          # Note: Advanced integration tests with package dependencies
          # should be run manually with `nix develop .#testing` and then `bats tests/unit/`
        };
      }
    )
    // {
      # Overlay for use in other flakes - allows importing tmux-proj easily
      overlays.default = final: prev: {
        # Make project packages available in nixpkgs
        project = final.callPackage (
          { buildGoModule }:
          buildGoModule rec {
            pname = "project";
            version = projectVersion;
            src = ./.;
            vendorHash = "sha256-wZZsEXvuLfPDO4/+Rbr4qZxPsDLOrKC/4nKwoubfYV8=";
            buildFlags = [ "-mod=mod" ];
            ldflags = [
              "-s"
              "-w"
              "-X main.version=${version}"
            ];
            subPackages = [ "cmd/proj" ];
            meta = with final.lib; {
              description = "A Git-based project management tool with zoxide-like navigation";
              homepage = "https://github.com/gfanton/project";
              license = licenses.mit;
              maintainers = [ "gfanton" ];
              mainProgram = "proj";
            };
          }
        ) { };

        # Make tmux plugin easily available
        tmux-proj = final.tmuxPlugins.mkTmuxPlugin rec {
          pluginName = "tmux-proj";
          version = projectVersion;
          rtpFilePath = "proj-tmux.tmux";
          src = ./plugins/proj-tmux/plugin;

          # Dependencies - the plugin needs proj and proj-tmux binaries
          buildInputs = [ final.project ];
          nativeBuildInputs = [ final.makeWrapper ];

          postInstall = ''
            # Note: The consuming nixpkgs config should ensure proj is in PATH
            # This plugin expects proj and proj-tmux to be available
          '';

          meta = with final.lib; {
            description = "A tmux plugin for proj - Git-based project management with session integration";
            homepage = "https://github.com/gfanton/project";
            license = licenses.mit;
            platforms = platforms.unix;
            maintainers = [ "gfanton" ];
          };
        };
      };
    };
}
