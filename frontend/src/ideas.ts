// Topic ideas for a new workspace. Picking one only fills in the topic: the
// coach still interviews the learner and plans the curriculum with them.
// Every idea must be something tests can check.

export type Idea = {
  title: string;
  blurb: string;
  /** One of workspace.Icons. */
  icon: string;
  /** The language, or "Any language" when the coach and learner pick one. */
  language: string;
};

export const IDEAS: Idea[] = [
  { title: "Build your own shell", blurb: "Parse commands, run programs, and handle pipes and builtins.", icon: "terminal", language: "Any language" },
  { title: "Build your own Redis", blurb: "A key-value server: the wire protocol, expiry and persistence.", icon: "redis", language: "Any language" },
  { title: "Build your own HTTP server", blurb: "Parse requests, route them and write responses by hand.", icon: "network", language: "Any language" },
  { title: "Build an interpreter", blurb: "Tokens, a parser, a syntax tree and a tree-walking evaluator.", icon: "braces", language: "Any language" },
  { title: "Build your own grep", blurb: "A regex engine: character classes, anchors and backtracking.", icon: "regex", language: "Any language" },
  { title: "Build your own Git", blurb: "Blobs, trees and commits, stored the way Git stores them.", icon: "git", language: "Any language" },
  { title: "Learn Go", blurb: "Types, errors, interfaces and goroutines, through small programs.", icon: "go", language: "Go" },
  { title: "Python for beginners", blurb: "Variables, functions, loops and data, from the very start.", icon: "python", language: "Python" },
  { title: "Learn Rust", blurb: "Ownership, borrowing, enums and traits.", icon: "rust", language: "Rust" },
  { title: "SQL with SQLite", blurb: "Queries, joins and aggregates against real tables.", icon: "sqlite", language: "SQL" },
  { title: "Data structures and algorithms", blurb: "Lists, trees, graphs, sorting and search, with their costs.", icon: "tree", language: "Python" },
  { title: "Memory management in C", blurb: "Pointers, malloc and free, and the bugs between them.", icon: "c", language: "C" },
];
