"use client";

import { useState } from "react";

const command =
  "curl -fsSL https://raw.githubusercontent.com/zozo123/wasted-cycles/main/run | sh";

const bars = [
  { label: "Model work", value: "1h 04m", width: "91%", tone: "purple" },
  { label: "Code changes", value: "1h 10m", width: "100%", tone: "lime" },
  { label: "Read & search", value: "34m", width: "49%", tone: "blue" },
  { label: "Local verify", value: "40m", width: "57%", tone: "teal" },
  { label: "Waiting for CI", value: "24m", width: "34%", tone: "orange" },
  { label: "Waiting for human", value: "17m", width: "24%", tone: "red" },
  { label: "Retries", value: "8m", width: "11%", tone: "rose" },
];

export default function Home() {
  const [copied, setCopied] = useState(false);

  async function copyCommand() {
    await navigator.clipboard.writeText(command);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1800);
  }

  return (
    <main>
      <nav className="nav shell">
        <a className="brand" href="#top" aria-label="Wasted Cycles home">
          <span>WASTED</span> CYCLES
        </a>
        <div className="nav-links">
          <a href="#method">Method</a>
          <a href="https://github.com/zozo123/wasted-cycles">GitHub ↗</a>
        </div>
      </nav>

      <section className="hero shell" id="top">
        <div className="hero-copy">
          <p className="eyebrow">LOCAL WALL-CLOCK PROFILER FOR CODING AGENTS</p>
          <h1>
            Your agents are fast.
            <br />
            <em>Your harness is not.</em>
          </h1>
          <p className="lede">
            See how much run time goes to actual model work—and how much
            disappears into CI, retries, tool stalls, sub-agent joins, and
            waiting for you.
          </p>
          <div className="command-wrap">
            <code>{command}</code>
            <button onClick={copyCommand} type="button" aria-label="Copy run command">
              {copied ? "COPIED" : "COPY"}
            </button>
          </div>
          <p className="trust-line">
            No install <i /> No account <i /> No upload <i /> Deletes itself
            after exit
          </p>
        </div>

        <Terminal />
      </section>

      <section className="harnesses">
        <div className="shell harness-row" aria-label="Supported coding agents">
          <span>READS THE TRACES YOU ALREADY HAVE</span>
          <strong>CODEX</strong>
          <strong>CLAUDE CODE</strong>
          <strong>CURSOR</strong>
          <strong>GROK BUILD</strong>
        </div>
      </section>

      <section className="thesis shell" id="method">
        <p className="section-kicker">THE THROUGHPUT QUESTION</p>
        <h2>
          Tokens tell you what you spent.
          <br />
          <span>Time tells you why you waited.</span>
        </h2>
        <div className="thesis-grid">
          <p>
            Wasted Cycles reconstructs the critical path from timestamped local
            trace events. It separates reasoning, exploration, edits, and
            verification from recoverable waits and repeated work.
          </p>
          <div className="formula">
            <span>THROUGHPUT</span>
            <strong>productive observed time</strong>
            <b>÷</b>
            <strong>total observed time</strong>
          </div>
        </div>
      </section>

      <section className="steps shell">
        <article>
          <span>01</span>
          <h3>Read locally</h3>
          <p>Find recent traces across four harnesses. Prompts and code never leave your machine.</p>
        </article>
        <article>
          <span>02</span>
          <h3>Rebuild the clock</h3>
          <p>Turn emitted events into an honest histogram of where wall-clock time went.</p>
        </article>
        <article>
          <span>03</span>
          <h3>Fix the biggest leak</h3>
          <p>Rank the critical-path bottlenecks by recoverable time, with a concrete next move.</p>
        </article>
      </section>

      <section className="honesty shell">
        <div>
          <p className="section-kicker">BUILT FOR TRUST</p>
          <h2>Precise where possible.<br />Explicit where inferred.</h2>
        </div>
        <div className="honesty-copy">
          <p>
            Agent trace formats expose different timing detail. “Model work” is
            labeled as an inference proxy unless the harness records exact
            duration. Offline gaps are capped. Every classification is
            auditable through JSON.
          </p>
          <a href="https://github.com/zozo123/wasted-cycles#method">Read the method →</a>
        </div>
      </section>

      <section className="final-cta">
        <div className="shell">
          <p className="section-kicker">ONE COMMAND. ONE ANSWER.</p>
          <h2>Where did the run stop moving?</h2>
          <div className="command-wrap compact">
            <code>{command}</code>
            <button onClick={copyCommand} type="button">
              {copied ? "COPIED" : "COPY"}
            </button>
          </div>
        </div>
      </section>

      <footer className="shell">
        <a className="brand" href="#top"><span>WASTED</span> CYCLES</a>
        <p>Open source · MIT · Runs entirely on your machine</p>
        <a href="https://github.com/zozo123/wasted-cycles">Star on GitHub ↗</a>
      </footer>
    </main>
  );
}

function Terminal() {
  return (
    <div className="terminal" aria-label="Wasted Cycles terminal report preview">
      <div className="terminal-bar">
        <div><i /><i /><i /></div>
        <span>wasted-cycles — overview</span>
        <b>⌁</b>
      </div>
      <div className="terminal-body">
        <div className="term-brand"><span>WASTED</span> CYCLES <small>last 7d · 84 traces</small></div>
        <div className="term-tabs"><b>1 OVERVIEW</b><span>2 HISTOGRAM</span><span>3 RUNS</span><span>4 METHOD</span></div>
        <div className="metrics">
          <div><small>OBSERVED</small><strong>4h 36m</strong></div>
          <div><small>REASONING</small><strong className="purple-text">23%</strong></div>
          <div><small>RECOVERABLE</small><strong className="red-text">1h 08m</strong></div>
          <div><small>THROUGHPUT</small><strong className="lime-text">75%</strong></div>
        </div>
        <p className="term-title">WHERE THE TIME WENT <span>7 classified buckets</span></p>
        <div className="bars">
          {bars.map((bar) => (
            <div className="bar-row" key={bar.label}>
              <span>{bar.label}</span>
              <div><i className={bar.tone} style={{ width: bar.width }} /></div>
              <b>{bar.value}</b>
            </div>
          ))}
        </div>
        <div className="terminal-finding">
          <span>BIGGEST LEAK</span>
          <strong>CI feedback · 24m on the critical path</strong>
          <small>Split independent checks and mirror the slow gate locally.</small>
        </div>
        <div className="term-footer">←/→ switch view <span>local only · no uploads</span></div>
      </div>
    </div>
  );
}
