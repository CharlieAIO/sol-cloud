class SolCloud < Formula
  desc "Deploy shared Solana test validators to Fly.io"
  homepage "https://github.com/CharlieAIO/sol-cloud"
  url "https://github.com/CharlieAIO/sol-cloud/archive/refs/heads/main.tar.gz"
  version "0.0.0"
  sha256 :no_check
  license "MIT"
  head "https://github.com/CharlieAIO/sol-cloud.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X github.com/CharlieAIO/sol-cloud/cmd.version=dev
    ]
    system "go", "build", *std_go_args(ldflags:), "."
  end

  test do
    output = shell_output("#{bin}/sol-cloud --help")
    assert_match "Deploy Solana validators", output
  end
end
