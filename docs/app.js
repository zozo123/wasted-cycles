const command =
  "curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run | sh";

for (const button of document.querySelectorAll(".copy-command")) {
  button.addEventListener("click", async () => {
    await navigator.clipboard.writeText(command);
    for (const peer of document.querySelectorAll(".copy-command")) {
      peer.textContent = "COPIED";
    }
    window.setTimeout(() => {
      for (const peer of document.querySelectorAll(".copy-command")) {
        peer.textContent = "COPY";
      }
    }, 1800);
  });
}
