class SolCloud < Formula
  desc "Deploy shared Solana test validators to Fly.io"
  homepage "https://github.com/CharlieAIO/sol-cloud"
  url "https://github.com/CharlieAIO/sol-cloud/archive/refs/tags/v1.0.0.tar.gz"
  version "1.0.0"
  sha256 "6a7d5a4e7da1da4024d9a84ae5ed7e1c2801abad16feb61dd7d0044479797ee6"
  license "MIT"
  head "https://github.com/CharlieAIO/sol-cloud.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X github.com/CharlieAIO/sol-cloud/cmd.version=v#{version}
    ]
    system "go", "build", *std_go_args(ldflags:), "."
  end

  test do
    output = shell_output("#{bin}/sol-cloud --help")
    assert_match "Deploy Solana validators", output
  end
end
