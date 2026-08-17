const [projectId] = process.argv
  .slice(2)
  .filter((arg) => !arg.startsWith("--"));
if (!projectId)
  throw new Error(
    "Usage: npm run snapshot -- --project <project-id> --pdf ./build/main.pdf",
  );
console.error(
  "This optional hook requires an authenticated local session cookie. It does not modify TeX sources or Git state.",
);
