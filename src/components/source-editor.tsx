"use client";

import dynamic from "next/dynamic";
import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  Blocks,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  Code2,
  Cpu,
  FileCode2,
  Files,
  Folder,
  FolderOpen,
  GitBranch,
  PanelLeft,
  RefreshCw,
  Save,
  Search,
  Settings,
  X,
} from "lucide-react";
import { loader } from "@monaco-editor/react";
import type { OnMount } from "@monaco-editor/react";
import type { IPosition, editor as MonacoEditorApi } from "monaco-editor";
import {
  defaultTexLiveConfig,
  type TexLiveConfig,
  type TexLiveEnvironment,
} from "@/lib/texlive-shared";

const MonacoEditor = dynamic(() => import("@monaco-editor/react"), {
  ssr: false,
  loading: () => <div className="source-editor-loading">正在加载编辑器...</div>,
});

if (typeof window !== "undefined")
  loader.config({ paths: { vs: "/api/editor-assets" } });

type EditorFile = {
  path: string;
  size: number;
  updatedAt: number;
  tooLarge: boolean;
};

type GitFileStatus = {
  path: string;
  code: string;
  kind:
    | "modified"
    | "untracked"
    | "added"
    | "deleted"
    | "renamed"
    | "copied"
    | "conflict"
    | "changed";
  label: string;
  description: string;
};

type FileTreeNode = {
  name: string;
  path: string;
  type: "directory" | "file";
  children?: FileTreeNode[];
  file?: EditorFile;
};

type Project = {
  name: string;
};

type GitSettings = {
  userName: string;
  userEmail: string;
  hasAccessToken: boolean;
  hasSshPrivateKey: boolean;
};

type PublicTexLiveProfile = {
  id: string;
  name: string;
  engine: string;
  buildTool: string;
  outputDirectory: string;
  shellEscape: boolean;
  texliveBinConfigured: boolean;
};

const texLiveToolLabels = {
  pdflatex: "pdfLaTeX",
  xelatex: "XeLaTeX",
  lualatex: "LuaLaTeX",
  latexmk: "latexmk",
  tectonic: "Tectonic",
} as const;

type InstalledExtension = {
  id: string;
  name: string;
  publisher: string;
  version: string;
  description: string;
  icon: "latex" | "git";
  settings: "texlive" | null;
};

const installedExtensions: InstalledExtension[] = [
  {
    id: "James-Yu.latex-workshop",
    name: "LaTeX Workshop",
    publisher: "James-Yu",
    version: "10.12.0",
    description: "LaTeX 源文件补全、构建和 SyncTeX 工作流",
    icon: "latex",
    settings: "texlive",
  },
];

type GitStatus = {
  branch: string | null;
  shortSha: string | null;
  message: string | null;
  author: string | null;
  dirty: boolean;
} | null;

type CursorPosition = {
  line: number;
  column: number;
};

function languageFor(path: string) {
  const extension = path.split(".").pop()?.toLowerCase();
  switch (extension) {
    case "bbx":
    case "cbx":
    case "cfg":
    case "cls":
    case "sty":
    case "tex":
      return "latex";
    case "json":
      return "json";
    case "md":
      return "markdown";
    case "xml":
      return "xml";
    case "yaml":
    case "yml":
      return "yaml";
    case "sh":
      return "shell";
    default:
      return "plaintext";
  }
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function indexGitStatuses(statuses: GitFileStatus[]) {
  return Object.fromEntries(
    statuses.map((status) => [status.path, status]),
  ) as Record<string, GitFileStatus>;
}

function buildFileTree(files: EditorFile[]) {
  const root: FileTreeNode = {
    name: "",
    path: "",
    type: "directory",
    children: [],
  };

  for (const file of files) {
    const parts = file.path.split("/");
    let parent = root;
    let currentPath = "";
    for (const [index, part] of parts.entries()) {
      currentPath = currentPath ? `${currentPath}/${part}` : part;
      const isFile = index === parts.length - 1;
      let child = parent.children?.find((item) => item.name === part);
      if (!child) {
        child = {
          name: part,
          path: currentPath,
          type: isFile ? "file" : "directory",
          ...(isFile ? { file } : { children: [] }),
        };
        parent.children?.push(child);
      }
      parent = child;
    }
  }

  const sortTree = (nodes: FileTreeNode[]) => {
    nodes.sort((left, right) => {
      if (left.type !== right.type) return left.type === "directory" ? -1 : 1;
      return left.name.localeCompare(right.name, "zh-CN", {
        numeric: true,
        sensitivity: "base",
      });
    });
    for (const node of nodes) {
      if (node.children) sortTree(node.children);
    }
  };
  sortTree(root.children ?? []);
  return root.children ?? [];
}

function filterFileTree(nodes: FileTreeNode[], query: string): FileTreeNode[] {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return nodes;
  const result: FileTreeNode[] = [];
  for (const node of nodes) {
    if (node.type === "file") {
      if (node.path.toLowerCase().includes(normalized)) result.push(node);
      continue;
    }
    const children = filterFileTree(node.children ?? [], normalized);
    if (children.length || node.path.toLowerCase().includes(normalized))
      result.push({ ...node, children });
  }
  return result;
}

function ancestorDirectories(filePath: string) {
  const parts = filePath.split("/");
  const directories = new Set<string>();
  let current = "";
  for (let index = 0; index < parts.length - 1; index += 1) {
    current = current ? `${current}/${parts[index]}` : parts[index];
    directories.add(current);
  }
  return directories;
}

const latexCommandSuggestions = [
  ["\\begin", "\\begin{${1:environment}}$0"],
  ["\\cite", "\\cite{${1:key}}$0"],
  ["\\documentclass", "\\documentclass[${1:options}]{${2:article}}$0"],
  ["\\emph", "\\emph{${1:text}}$0"],
  ["\\end", "\\end{${1:environment}}$0"],
  ["\\frac", "\\frac{${1:numerator}}{${2:denominator}}$0"],
  [
    "\\includegraphics",
    "\\includegraphics[${1:width=\\linewidth}]{${2:file}}$0",
  ],
  ["\\item", "\\item ${1:text}$0"],
  ["\\label", "\\label{${1:name}}$0"],
  ["\\section", "\\section{${1:title}}$0"],
  ["\\subsection", "\\subsection{${1:title}}$0"],
  ["\\textbf", "\\textbf{${1:text}}$0"],
  ["\\textit", "\\textit{${1:text}}$0"],
  ["\\texttt", "\\texttt{${1:text}}$0"],
  ["\\usepackage", "\\usepackage[${1:options}]{${2:package}}$0"],
  ["\\ref", "\\ref{${1:name}}$0"],
  ["\\url", "\\url{${1:https://example.com}}$0"],
] as const;
const latexEnvironmentSuggestions = [
  "abstract",
  "align",
  "equation",
  "enumerate",
  "figure",
  "itemize",
  "proof",
  "table",
  "tabular",
  "theorem",
];
let latexCompletionRegistered = false;

function registerLatexCompletions(monaco: Parameters<OnMount>[1]) {
  if (latexCompletionRegistered) return;
  latexCompletionRegistered = true;
  monaco.languages.registerCompletionItemProvider("latex", {
    triggerCharacters: ["\\", "{"],
    provideCompletionItems(
      model: MonacoEditorApi.ITextModel,
      position: IPosition,
    ) {
      const line = model
        .getLineContent(position.lineNumber)
        .slice(0, position.column - 1);
      const environmentMatch = /\\begin\{([A-Za-z]*)$/.exec(line);
      if (environmentMatch) {
        const query = environmentMatch[1].toLowerCase();
        return {
          suggestions: latexEnvironmentSuggestions
            .filter((value) => value.startsWith(query))
            .map((value) => ({
              label: value,
              kind: monaco.languages.CompletionItemKind.Class,
              insertText: value,
              range: {
                startLineNumber: position.lineNumber,
                startColumn: position.column - query.length,
                endLineNumber: position.lineNumber,
                endColumn: position.column,
              },
            })),
        };
      }
      const commandMatch = /\\([A-Za-z]*)$/.exec(line);
      const query = commandMatch?.[1].toLowerCase() ?? "";
      return {
        suggestions: latexCommandSuggestions
          .filter(([label]) => label.slice(1).toLowerCase().startsWith(query))
          .map(([label, insertText]) => ({
            label,
            kind: monaco.languages.CompletionItemKind.Keyword,
            insertText,
            insertTextRules:
              monaco.languages.CompletionItemInsertTextRule.InsertAsSnippet,
            range: commandMatch
              ? {
                  startLineNumber: position.lineNumber,
                  startColumn: position.column - query.length,
                  endLineNumber: position.lineNumber,
                  endColumn: position.column,
                }
              : undefined,
          })),
      };
    },
  });
}

type FileTreeItemProps = {
  node: FileTreeNode;
  depth: number;
  expandedDirectories: Set<string>;
  selectedPath: string;
  dirty: boolean;
  gitStatuses: Record<string, GitFileStatus>;
  onToggleDirectory: (path: string) => void;
  onSelectFile: (file: EditorFile) => void;
};

function FileTreeItem({
  node,
  depth,
  expandedDirectories,
  selectedPath,
  dirty,
  gitStatuses,
  onToggleDirectory,
  onSelectFile,
}: FileTreeItemProps) {
  if (node.type === "directory") {
    const expanded = expandedDirectories.has(node.path);
    return (
      <div role="treeitem" aria-expanded={expanded} aria-selected={false}>
        <button
          className="source-editor-tree-row source-editor-tree-folder"
          style={{ paddingLeft: `${8 + depth * 14}px` }}
          type="button"
          onClick={() => onToggleDirectory(node.path)}
        >
          {expanded ? (
            <ChevronDown size={14} aria-hidden="true" />
          ) : (
            <ChevronRight size={14} aria-hidden="true" />
          )}
          {expanded ? (
            <FolderOpen size={16} aria-hidden="true" />
          ) : (
            <Folder size={16} aria-hidden="true" />
          )}
          <span>{node.name}</span>
        </button>
        {expanded && (
          <div>
            {(node.children ?? []).map((child) => (
              <FileTreeItem
                depth={depth + 1}
                dirty={dirty}
                expandedDirectories={expandedDirectories}
                gitStatuses={gitStatuses}
                key={child.path}
                node={child}
                onSelectFile={onSelectFile}
                onToggleDirectory={onToggleDirectory}
                selectedPath={selectedPath}
              />
            ))}
          </div>
        )}
      </div>
    );
  }

  const file = node.file;
  if (!file) return null;
  const selected = file.path === selectedPath;
  const gitStatus = gitStatuses[file.path] ?? null;
  return (
    <button
      aria-selected={selected}
      className={`source-editor-tree-row source-editor-tree-file${selected ? " selected" : ""}`}
      disabled={file.tooLarge}
      role="treeitem"
      style={{ paddingLeft: `${25 + depth * 14}px` }}
      title={
        file.tooLarge
          ? "文件超过 2 MB，无法在浏览器中打开"
          : `${formatBytes(file.size)} · ${file.path}`
      }
      type="button"
      onClick={() => onSelectFile(file)}
    >
      <FileCode2 size={15} aria-hidden="true" />
      <span className="source-editor-tree-name">{node.name}</span>
      <span className="source-editor-tree-meta">
        {selected && dirty && (
          <span className="source-editor-tree-dirty" title="未保存修改">
            ●
          </span>
        )}
        {gitStatus && (
          <span
            className={
              "source-editor-git-status source-editor-git-status-" +
              gitStatus.kind
            }
            title={gitStatus.description + "（" + gitStatus.code + "）"}
          >
            {gitStatus.label}
          </span>
        )}
        {file.tooLarge && <small>过大</small>}
      </span>
    </button>
  );
}

export function SourceEditor({ projectId }: { projectId: string }) {
  const [project, setProject] = useState<Project | null>(null);
  const [files, setFiles] = useState<EditorFile[]>([]);
  const [selectedPath, setSelectedPath] = useState("");
  const [loadedContent, setLoadedContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [fileLoading, setFileLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const [filter, setFilter] = useState("");
  const [sidebarView, setSidebarView] = useState<
    "explorer" | "search" | "source-control" | "extensions" | "settings"
  >("explorer");
  const [settingsSection, setSettingsSection] = useState<"git" | "texlive">(
    "git",
  );
  const [extensionFilter, setExtensionFilter] = useState("");
  const [gitSettings, setGitSettings] = useState<GitSettings | null>(null);
  const [gitStatus, setGitStatus] = useState<GitStatus>(null);
  const [gitFileStatuses, setGitFileStatuses] = useState<
    Record<string, GitFileStatus>
  >({});
  const [gitForm, setGitForm] = useState({
    userName: "",
    userEmail: "",
    accessToken: "",
    sshPrivateKey: "",
    clearAccessToken: false,
    clearSshPrivateKey: false,
  });
  const [gitLoading, setGitLoading] = useState(false);
  const [gitSaving, setGitSaving] = useState(false);
  const [texLiveConfig, setTexLiveConfig] =
    useState<TexLiveConfig>(defaultTexLiveConfig);
  const [texLiveProfiles, setTexLiveProfiles] = useState<
    PublicTexLiveProfile[]
  >([]);
  const [texLiveEnvironment, setTexLiveEnvironment] =
    useState<TexLiveEnvironment | null>(null);
  const [texLiveLoading, setTexLiveLoading] = useState(false);
  const [texLiveSaving, setTexLiveSaving] = useState(false);
  const [expandedDirectories, setExpandedDirectories] = useState<Set<string>>(
    () => new Set(),
  );
  const [cursor, setCursor] = useState<CursorPosition>({ line: 1, column: 1 });
  const selectedPathRef = useRef("");
  const contentRef = useRef("");
  const savedContentRef = useRef("");
  const dirtyRef = useRef(false);
  const savingRef = useRef(false);
  const loadRequestRef = useRef(0);
  const editorRef = useRef<Parameters<OnMount>[0] | null>(null);
  const saveRef = useRef<() => Promise<void>>(async () => undefined);

  const tree = useMemo(() => buildFileTree(files), [files]);
  const visibleTree = useMemo(
    () => filterFileTree(tree, filter),
    [filter, tree],
  );
  const visibleExtensions = useMemo(() => {
    const query = extensionFilter.trim().toLowerCase();
    if (!query) return installedExtensions;
    return installedExtensions.filter((extension) =>
      [
        extension.id,
        extension.name,
        extension.publisher,
        extension.description,
      ].some((value) => value.toLowerCase().includes(query)),
    );
  }, [extensionFilter]);

  const setDirtyValue = useCallback((value: boolean) => {
    dirtyRef.current = value;
    setDirty(value);
  }, []);

  const discardCurrentEdits = useCallback(() => {
    if (!dirtyRef.current || !editorRef.current) return;
    editorRef.current.setValue(savedContentRef.current);
    contentRef.current = savedContentRef.current;
    setDirtyValue(false);
  }, [setDirtyValue]);

  const loadFile = useCallback(
    async (path: string) => {
      const requestId = ++loadRequestRef.current;
      setFileLoading(true);
      setError("");
      try {
        const response = await fetch(
          `/api/projects/${projectId}/editor?path=${encodeURIComponent(path)}`,
        );
        const body = await response.json();
        if (!response.ok)
          throw new Error(body.error?.message ?? "文件读取失败");
        if (requestId !== loadRequestRef.current) return;
        const file = body.file as { path: string; content: string };
        const samePath = selectedPathRef.current === file.path;
        selectedPathRef.current = file.path;
        contentRef.current = file.content;
        savedContentRef.current = file.content;
        if (samePath && editorRef.current?.getValue() !== file.content)
          editorRef.current?.setValue(file.content);
        setSelectedPath(file.path);
        setLoadedContent(file.content);
        setDirtyValue(false);
        setCursor({ line: 1, column: 1 });
        setStatus("");
      } catch (reason) {
        if (requestId === loadRequestRef.current)
          setError(reason instanceof Error ? reason.message : "文件读取失败");
      } finally {
        if (requestId === loadRequestRef.current) setFileLoading(false);
      }
    },
    [projectId, setDirtyValue],
  );

  const loadFiles = useCallback(async () => {
    const currentPath = selectedPathRef.current;
    setLoading(true);
    setError("");
    try {
      const [projectResponse, filesResponse] = await Promise.all([
        fetch(`/api/projects/${projectId}`),
        fetch(`/api/projects/${projectId}/editor`),
      ]);
      const projectBody = await projectResponse.json();
      const filesBody = await filesResponse.json();
      if (!projectResponse.ok)
        throw new Error(projectBody.error?.message ?? "项目读取失败");
      if (!filesResponse.ok)
        throw new Error(filesBody.error?.message ?? "源文件列表读取失败");
      const nextFiles = (filesBody.files ?? []) as EditorFile[];
      setProject(projectBody.project);
      setFiles(nextFiles);
      setGitFileStatuses(
        indexGitStatuses((filesBody.gitStatuses ?? []) as GitFileStatus[]),
      );
      const preferred =
        nextFiles.find((file) => file.path === currentPath) ??
        nextFiles.find((file) => file.path === "main.tex") ??
        nextFiles.find((file) => file.path.toLowerCase().endsWith(".tex")) ??
        nextFiles[0];
      if (preferred) {
        setExpandedDirectories((current) => {
          const next = new Set(current);
          for (const directory of ancestorDirectories(preferred.path))
            next.add(directory);
          return next;
        });
        await loadFile(preferred.path);
      } else {
        selectedPathRef.current = "";
        contentRef.current = "";
        savedContentRef.current = "";
        setSelectedPath("");
        setLoadedContent("");
        setDirtyValue(false);
      }
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "源文件加载失败");
    } finally {
      setLoading(false);
    }
  }, [loadFile, projectId, setDirtyValue]);

  useEffect(() => {
    void loadFiles();
  }, [loadFiles]);

  const loadGitSettings = useCallback(async () => {
    setGitLoading(true);
    try {
      const response = await fetch(`/api/projects/${projectId}/git-config`);
      const body = await response.json();
      if (!response.ok)
        throw new Error(body.error?.message ?? "Git 配置读取失败");
      setGitSettings(body.settings as GitSettings);
      setGitStatus((body.git as GitStatus) ?? null);
      setGitForm((current) => ({
        ...current,
        userName: body.settings?.userName ?? "",
        userEmail: body.settings?.userEmail ?? "",
      }));
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Git 配置读取失败");
    } finally {
      setGitLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    void loadGitSettings();
  }, [loadGitSettings]);

  const loadTexLiveSettings = useCallback(async () => {
    setTexLiveLoading(true);
    setError("");
    try {
      const [response, profilesResponse] = await Promise.all([
        fetch("/api/projects/" + projectId + "/texlive-config"),
        fetch("/api/texlive-profiles", { cache: "no-store" }),
      ]);
      const body = await response.json();
      if (!response.ok)
        throw new Error(body.error?.message ?? "TeX Live 配置读取失败");
      setTexLiveConfig(body.config as TexLiveConfig);
      setTexLiveEnvironment((body.environment as TexLiveEnvironment) ?? null);
      if (profilesResponse.ok) {
        const profilesBody = (await profilesResponse.json()) as {
          profiles?: PublicTexLiveProfile[];
        };
        setTexLiveProfiles(profilesBody.profiles ?? []);
      }
    } catch (reason) {
      setError(
        reason instanceof Error ? reason.message : "TeX Live 配置读取失败",
      );
    } finally {
      setTexLiveLoading(false);
    }
  }, [projectId]);

  useEffect(() => {
    if (sidebarView === "settings" && settingsSection === "texlive")
      void loadTexLiveSettings();
  }, [loadTexLiveSettings, settingsSection, sidebarView]);

  const saveGitSettings = useCallback(async () => {
    setGitSaving(true);
    setError("");
    try {
      const response = await fetch(`/api/projects/${projectId}/git-config`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(gitForm),
      });
      const body = await response.json();
      if (!response.ok)
        throw new Error(body.error?.message ?? "Git 配置保存失败");
      setGitSettings(body.settings as GitSettings);
      setGitForm((current) => ({
        ...current,
        accessToken: "",
        sshPrivateKey: "",
        clearAccessToken: false,
        clearSshPrivateKey: false,
      }));
      setStatus("项目 Git 配置已保存");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Git 配置保存失败");
    } finally {
      setGitSaving(false);
    }
  }, [gitForm, projectId]);

  const saveTexLiveSettings = useCallback(async () => {
    setTexLiveSaving(true);
    setError("");
    try {
      const response = await fetch(
        "/api/projects/" + projectId + "/texlive-config",
        {
          method: "PUT",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(texLiveConfig),
        },
      );
      const body = await response.json();
      if (!response.ok)
        throw new Error(body.error?.message ?? "TeX Live 配置保存失败");
      setTexLiveConfig(body.config as TexLiveConfig);
      setStatus("项目 TeX Live 配置已保存");
    } catch (reason) {
      setError(
        reason instanceof Error ? reason.message : "TeX Live 配置保存失败",
      );
    } finally {
      setTexLiveSaving(false);
    }
  }, [projectId, texLiveConfig]);

  const saveFile = useCallback(async () => {
    const path = selectedPathRef.current;
    const content = contentRef.current;
    if (
      !path ||
      savingRef.current ||
      content === savedContentRef.current ||
      fileLoading
    )
      return;
    savingRef.current = true;
    setSaving(true);
    setError("");
    try {
      const response = await fetch(`/api/projects/${projectId}/editor`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path, content }),
      });
      const body = await response.json();
      if (!response.ok) throw new Error(body.error?.message ?? "文件保存失败");
      savedContentRef.current = content;
      setDirtyValue(false);
      if (Array.isArray(body.gitStatuses))
        setGitFileStatuses(
          indexGitStatuses(body.gitStatuses as GitFileStatus[]),
        );
      setFiles((current) =>
        current.map((file) =>
          file.path === path
            ? {
                ...file,
                size: body.file.size,
                updatedAt: body.file.updatedAt,
                tooLarge: false,
              }
            : file,
        ),
      );
      setStatus(`已保存 ${path}`);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "文件保存失败");
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  }, [fileLoading, projectId, setDirtyValue]);

  saveRef.current = saveFile;

  const selectFile = useCallback(
    async (file: EditorFile) => {
      if (file.path === selectedPathRef.current || file.tooLarge) {
        if (file.tooLarge) setError("文件超过在线编辑器大小上限，无法打开。");
        return;
      }
      if (
        dirtyRef.current &&
        !window.confirm(
          "当前文件有未保存修改，切换文件会丢失这些修改。继续吗？",
        )
      )
        return;
      discardCurrentEdits();
      await loadFile(file.path);
    },
    [discardCurrentEdits, loadFile],
  );

  const handleEditorMount: OnMount = useCallback((editor, monaco) => {
    editorRef.current = editor;
    if (
      !monaco.languages
        .getLanguages()
        .some((language: { id: string }) => language.id === "latex")
    ) {
      monaco.languages.register({
        aliases: ["LaTeX", "TeX"],
        extensions: [".tex", ".sty", ".cls"],
        id: "latex",
      });
      monaco.languages.setMonarchTokensProvider("latex", {
        brackets: [
          ["{", "}", "delimiter.curly"],
          ["[", "]", "delimiter.square"],
          ["(", ")", "delimiter.parenthesis"],
        ],
        tokenizer: {
          root: [
            [
              /\\(?:begin|end|documentclass|usepackage|input|include)\b/,
              "keyword",
            ],
            [/\\[a-zA-Z@]+/, "keyword"],
            [/%.*$/, "comment"],
            [/\$[^$]*\$/, "string"],
            [/\^|_/, "operator"],
            [/[{}[\]()]/, "@brackets"],
          ],
        },
      });
    }
    registerLatexCompletions(monaco);
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
      void saveRef.current();
    });
    editor.onDidChangeCursorPosition((event) => {
      setCursor({
        line: event.position.lineNumber,
        column: event.position.column,
      });
    });
  }, []);

  const onEditorChange = useCallback(
    (value: string | undefined) => {
      contentRef.current = value ?? "";
      setDirtyValue(contentRef.current !== savedContentRef.current);
      setStatus("");
    },
    [setDirtyValue],
  );

  const toggleDirectory = useCallback((path: string) => {
    setExpandedDirectories((current) => {
      const next = new Set(current);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }, []);

  const refreshFiles = useCallback(() => {
    if (
      dirtyRef.current &&
      !window.confirm("当前文件有未保存修改，刷新会丢失这些修改。继续吗？")
    )
      return;
    discardCurrentEdits();
    void loadFiles();
  }, [discardCurrentEdits, loadFiles]);

  return (
    <main className="source-editor-vscode">
      <header className="source-editor-titlebar">
        <div className="source-editor-titlebar-left">
          <Link
            className="source-editor-titlebar-back"
            href={`/projects/${projectId}`}
            title="返回项目首页"
          >
            <ArrowLeft size={15} aria-hidden="true" />
            <span>项目首页</span>
          </Link>
          <span className="source-editor-titlebar-divider">/</span>
          <span className="source-editor-project-name" title={project?.name}>
            {project?.name ?? "论文源文件"}
          </span>
        </div>
        <div className="source-editor-titlebar-current">
          {selectedPath || "源文件编辑器"}
        </div>
        <div className="source-editor-titlebar-actions">
          <Link
            className="source-editor-titlebar-icon"
            href={`/projects/${projectId}/review`}
            title="打开审阅工作区"
          >
            <Code2 size={16} aria-hidden="true" />
          </Link>
          <button
            className="source-editor-titlebar-icon"
            disabled={!selectedPath || !dirty || saving || fileLoading}
            title="保存当前文件（Ctrl/Cmd+S）"
            type="button"
            onClick={() => void saveFile()}
          >
            <Save size={16} aria-hidden="true" />
          </button>
        </div>
      </header>

      <div className="source-editor-workbench">
        <aside className="source-editor-activitybar" aria-label="编辑器导航">
          <button
            className={sidebarView === "explorer" ? "active" : ""}
            title="资源管理器"
            type="button"
            onClick={() => setSidebarView("explorer")}
          >
            <Files size={21} aria-hidden="true" />
          </button>
          <button
            className={sidebarView === "search" ? "active" : ""}
            title="筛选文件"
            type="button"
            onClick={() => setSidebarView("search")}
          >
            <Search size={20} aria-hidden="true" />
          </button>
          <button
            className={sidebarView === "source-control" ? "active" : ""}
            title="源代码管理"
            type="button"
            onClick={() => setSidebarView("source-control")}
          >
            <GitBranch size={20} aria-hidden="true" />
          </button>
          <button
            className={sidebarView === "extensions" ? "active" : ""}
            title="扩展"
            type="button"
            onClick={() => setSidebarView("extensions")}
          >
            <Blocks size={20} aria-hidden="true" />
          </button>
          <div className="source-editor-activity-spacer" />
          <Link title="打开审阅工作区" href={`/projects/${projectId}/review`}>
            <Code2 size={19} aria-hidden="true" />
          </Link>
          <Link title="返回项目首页" href={`/projects/${projectId}`}>
            <PanelLeft size={19} aria-hidden="true" />
          </Link>
          <button
            className={sidebarView === "settings" ? "active" : ""}
            title="项目设置（Git / TeX Live）"
            type="button"
            onClick={() => setSidebarView("settings")}
          >
            <Settings size={18} aria-hidden="true" />
          </button>
        </aside>

        <aside
          className="source-editor-sidebar"
          aria-label={
            sidebarView === "extensions" ? "扩展管理器" : "源文件管理器"
          }
        >
          {sidebarView === "extensions" ? (
            <div className="source-editor-sidebar-panel source-editor-extension-panel">
              <div className="source-editor-sidebar-heading">
                <span>扩展</span>
                <button
                  title="返回资源管理器"
                  type="button"
                  onClick={() => setSidebarView("explorer")}
                >
                  <X size={16} aria-hidden="true" />
                </button>
              </div>
              <label className="source-editor-extension-search">
                <Search size={15} aria-hidden="true" />
                <input
                  aria-label="搜索扩展"
                  placeholder="在扩展中搜索"
                  type="search"
                  value={extensionFilter}
                  onChange={(event) => setExtensionFilter(event.target.value)}
                />
              </label>
              <div className="source-editor-extension-content">
                <div className="source-editor-extension-section-heading">
                  <strong>已安装</strong>
                  <small>{visibleExtensions.length}</small>
                </div>
                {visibleExtensions.length ? (
                  <div className="source-editor-installed-extensions">
                    {visibleExtensions.map((extension) => (
                      <article
                        className="source-editor-extension-card"
                        key={extension.id}
                      >
                        <span className="source-editor-extension-icon">
                          {extension.icon === "latex" ? (
                            <FileCode2 size={22} aria-hidden="true" />
                          ) : (
                            <GitBranch size={22} aria-hidden="true" />
                          )}
                        </span>
                        <div className="source-editor-extension-card-main">
                          <strong>{extension.name}</strong>
                          <small>
                            {extension.publisher} · {extension.version}
                          </small>
                          <span>{extension.description}</span>
                        </div>
                        <div className="source-editor-extension-card-actions">
                          <span>已安装</span>
                          {extension.settings === "texlive" && (
                            <button
                              aria-label="打开 TeX Live 设置"
                              title="打开 TeX Live 设置"
                              type="button"
                              onClick={() => {
                                setSettingsSection("texlive");
                                setSidebarView("settings");
                              }}
                            >
                              <Settings size={15} aria-hidden="true" />
                            </button>
                          )}
                        </div>
                      </article>
                    ))}
                  </div>
                ) : (
                  <p className="source-editor-sidebar-empty">没有匹配的扩展</p>
                )}
              </div>
            </div>
          ) : sidebarView === "search" ? (
            <div className="source-editor-sidebar-panel">
              <div className="source-editor-sidebar-heading">
                <span>搜索</span>
                <button
                  title="返回资源管理器"
                  type="button"
                  onClick={() => setSidebarView("explorer")}
                >
                  <X size={16} aria-hidden="true" />
                </button>
              </div>
              <label className="source-editor-search-input">
                <Search size={15} aria-hidden="true" />
                <input
                  autoFocus
                  aria-label="筛选源文件"
                  placeholder="按文件名筛选"
                  type="search"
                  value={filter}
                  onChange={(event) => setFilter(event.target.value)}
                />
              </label>
              <p className="source-editor-sidebar-hint">
                {filter
                  ? `找到 ${visibleTree.length ? "匹配" : "无匹配"} 文件`
                  : "输入名称或路径开始筛选"}
              </p>
            </div>
          ) : sidebarView === "source-control" ? (
            <div className="source-editor-sidebar-panel source-editor-source-control">
              <div className="source-editor-sidebar-heading">
                <span>源代码管理</span>
                <button
                  title="刷新 Git 状态"
                  type="button"
                  onClick={() => void loadGitSettings()}
                >
                  <RefreshCw size={15} aria-hidden="true" />
                </button>
              </div>
              {gitLoading ? (
                <p className="source-editor-sidebar-empty">
                  正在读取 Git 状态...
                </p>
              ) : (
                <>
                  <div className="source-editor-git-summary">
                    <div>
                      <GitBranch size={16} aria-hidden="true" />
                      <strong>
                        {gitStatus?.branch ?? "未检测到 Git 仓库"}
                      </strong>
                    </div>
                    {gitStatus && (
                      <>
                        <span>{gitStatus.shortSha ?? "无提交"}</span>
                        <small>{gitStatus.message ?? "没有提交信息"}</small>
                        <small>
                          {gitStatus.dirty ? "有未提交修改" : "工作区干净"}
                        </small>
                      </>
                    )}
                  </div>
                  <button
                    className="source-editor-sidebar-action"
                    type="button"
                    onClick={() => {
                      setSettingsSection("git");
                      setSidebarView("settings");
                    }}
                  >
                    <Settings size={15} aria-hidden="true" />
                    配置项目 Git 身份与凭据
                  </button>
                  <button
                    className="source-editor-sidebar-action"
                    type="button"
                    onClick={() => {
                      setSettingsSection("texlive");
                      setSidebarView("settings");
                    }}
                  >
                    <Cpu size={15} aria-hidden="true" />
                    配置项目 TeX Live
                  </button>
                </>
              )}
            </div>
          ) : sidebarView === "settings" ? (
            <div className="source-editor-sidebar-panel source-editor-git-settings">
              <div className="source-editor-sidebar-heading">
                <span>项目设置</span>
                <button
                  title="返回资源管理器"
                  type="button"
                  onClick={() => setSidebarView("explorer")}
                >
                  <X size={16} aria-hidden="true" />
                </button>
              </div>
              <div className="source-editor-settings-tabs" role="tablist">
                <button
                  aria-selected={settingsSection === "git"}
                  className={
                    settingsSection === "git"
                      ? "source-editor-settings-tab active"
                      : "source-editor-settings-tab"
                  }
                  role="tab"
                  type="button"
                  onClick={() => setSettingsSection("git")}
                >
                  Git
                </button>
                <button
                  aria-selected={settingsSection === "texlive"}
                  className={
                    settingsSection === "texlive"
                      ? "source-editor-settings-tab active"
                      : "source-editor-settings-tab"
                  }
                  role="tab"
                  type="button"
                  onClick={() => setSettingsSection("texlive")}
                >
                  TeX Live
                </button>
              </div>
              {settingsSection === "git" ? (
                <>
                  <p className="source-editor-sidebar-hint">
                    仅当前项目使用。凭据加密保存，不读取宿主机的 Git 全局配置。
                  </p>
                  <form
                    className="source-editor-git-form"
                    onSubmit={(event) => {
                      event.preventDefault();
                      void saveGitSettings();
                    }}
                  >
                    <label>
                      提交姓名
                      <input
                        maxLength={120}
                        value={gitForm.userName}
                        onChange={(event) =>
                          setGitForm((current) => ({
                            ...current,
                            userName: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <label>
                      提交邮箱
                      <input
                        maxLength={254}
                        type="email"
                        value={gitForm.userEmail}
                        onChange={(event) =>
                          setGitForm((current) => ({
                            ...current,
                            userEmail: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <label>
                      GitHub Access Token
                      <input
                        autoComplete="new-password"
                        maxLength={500}
                        placeholder={
                          gitSettings?.hasAccessToken
                            ? "已保存，输入新值可替换"
                            : "不会回显"
                        }
                        type="password"
                        value={gitForm.accessToken}
                        onChange={(event) =>
                          setGitForm((current) => ({
                            ...current,
                            accessToken: event.target.value,
                            clearAccessToken: false,
                          }))
                        }
                      />
                      {gitSettings?.hasAccessToken && (
                        <span className="source-editor-checkbox">
                          <input
                            checked={gitForm.clearAccessToken}
                            type="checkbox"
                            onChange={(event) =>
                              setGitForm((current) => ({
                                ...current,
                                clearAccessToken: event.target.checked,
                              }))
                            }
                          />
                          清除已保存令牌
                        </span>
                      )}
                    </label>
                    <label>
                      SSH 提交私钥
                      <textarea
                        autoComplete="off"
                        maxLength={16_000}
                        placeholder={
                          gitSettings?.hasSshPrivateKey
                            ? "已保存，输入新值可替换"
                            : "粘贴 PEM 私钥，不会回显"
                        }
                        rows={6}
                        value={gitForm.sshPrivateKey}
                        onChange={(event) =>
                          setGitForm((current) => ({
                            ...current,
                            sshPrivateKey: event.target.value,
                            clearSshPrivateKey: false,
                          }))
                        }
                      />
                      {gitSettings?.hasSshPrivateKey && (
                        <span className="source-editor-checkbox">
                          <input
                            checked={gitForm.clearSshPrivateKey}
                            type="checkbox"
                            onChange={(event) =>
                              setGitForm((current) => ({
                                ...current,
                                clearSshPrivateKey: event.target.checked,
                              }))
                            }
                          />
                          清除已保存私钥
                        </span>
                      )}
                    </label>
                    <button
                      className="source-editor-sidebar-action"
                      disabled={gitSaving}
                      type="submit"
                    >
                      <Save size={15} aria-hidden="true" />
                      {gitSaving ? "保存中..." : "保存项目 Git 配置"}
                    </button>
                  </form>
                </>
              ) : (
                <div className="source-editor-texlive-settings">
                  <p className="source-editor-sidebar-hint">
                    只保存项目级编译偏好。当前版本不会自动执行编译，也不会修改
                    TeX 源文件。
                  </p>
                  <form
                    className="source-editor-git-form source-editor-texlive-form"
                    onSubmit={(event) => {
                      event.preventDefault();
                      void saveTexLiveSettings();
                    }}
                  >
                    <label>
                      管理员编译配置
                      <select
                        value={texLiveConfig.profileId}
                        onChange={(event) =>
                          setTexLiveConfig((current) => ({
                            ...current,
                            profileId: event.target.value,
                          }))
                        }
                      >
                        <option value="">使用项目手动设置</option>
                        {texLiveProfiles.map((profile) => (
                          <option key={profile.id} value={profile.id}>
                            {profile.name} · {profile.engine} /{" "}
                            {profile.buildTool}
                          </option>
                        ))}
                      </select>
                      {!texLiveProfiles.length && (
                        <small className="muted">
                          管理员尚未发布可用配置。
                        </small>
                      )}
                    </label>
                    <label>
                      编译引擎
                      <select
                        value={texLiveConfig.engine}
                        onChange={(event) =>
                          setTexLiveConfig((current) => ({
                            ...current,
                            engine: event.target
                              .value as TexLiveConfig["engine"],
                          }))
                        }
                      >
                        <option value="pdflatex">pdfLaTeX</option>
                        <option value="xelatex">XeLaTeX</option>
                        <option value="lualatex">LuaLaTeX</option>
                        <option value="tectonic">Tectonic</option>
                      </select>
                    </label>
                    <label>
                      构建工具
                      <select
                        value={texLiveConfig.buildTool}
                        onChange={(event) =>
                          setTexLiveConfig((current) => ({
                            ...current,
                            buildTool: event.target
                              .value as TexLiveConfig["buildTool"],
                          }))
                        }
                      >
                        <option value="latexmk">latexmk（推荐）</option>
                        <option value="tectonic">Tectonic</option>
                        <option value="engine">直接调用引擎</option>
                      </select>
                    </label>
                    <label>
                      输出目录
                      <input
                        maxLength={160}
                        placeholder="build"
                        value={texLiveConfig.outputDirectory}
                        onChange={(event) =>
                          setTexLiveConfig((current) => ({
                            ...current,
                            outputDirectory: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <label>
                      编译器目录
                      <input
                        maxLength={500}
                        placeholder="留空使用应用配置的默认编译环境"
                        type="password"
                        value={texLiveConfig.texliveBinPath}
                        onChange={(event) =>
                          setTexLiveConfig((current) => ({
                            ...current,
                            texliveBinPath: event.target.value,
                          }))
                        }
                      />
                    </label>
                    <label>
                      自动构建
                      <select
                        value={texLiveConfig.autoBuild}
                        onChange={(event) =>
                          setTexLiveConfig((current) => ({
                            ...current,
                            autoBuild: event.target
                              .value as TexLiveConfig["autoBuild"],
                          }))
                        }
                      >
                        <option value="off">关闭</option>
                        <option value="on-save">保存源文件时</option>
                        <option value="on-start">打开编辑器时</option>
                      </select>
                    </label>
                    <label>
                      PDF 预览方式
                      <select
                        value={texLiveConfig.pdfPreview}
                        onChange={(event) =>
                          setTexLiveConfig((current) => ({
                            ...current,
                            pdfPreview: event.target
                              .value as TexLiveConfig["pdfPreview"],
                          }))
                        }
                      >
                        <option value="review-hub">Review Hub 内置预览</option>
                        <option value="latex-workshop">
                          VS Code LaTeX Workshop
                        </option>
                        <option value="browser">外部浏览器</option>
                      </select>
                    </label>
                    <label className="source-editor-checkbox">
                      <input
                        checked={texLiveConfig.syncTex}
                        type="checkbox"
                        onChange={(event) =>
                          setTexLiveConfig((current) => ({
                            ...current,
                            syncTex: event.target.checked,
                          }))
                        }
                      />
                      启用 SyncTeX 正反向定位
                    </label>
                    <label className="source-editor-checkbox source-editor-warning-option">
                      <input
                        checked={texLiveConfig.shellEscape}
                        type="checkbox"
                        onChange={(event) =>
                          setTexLiveConfig((current) => ({
                            ...current,
                            shellEscape: event.target.checked,
                          }))
                        }
                      />
                      允许 --shell-escape
                      <small>仅在信任项目源码时启用</small>
                    </label>
                    <button
                      className="source-editor-sidebar-action"
                      disabled={texLiveSaving}
                      type="submit"
                    >
                      <Save size={15} aria-hidden="true" />
                      {texLiveSaving ? "保存中..." : "保存 TeX Live 配置"}
                    </button>
                  </form>
                  <div className="source-editor-texlive-detection">
                    <div className="source-editor-texlive-detection-heading">
                      <strong>环境检测</strong>
                      <button
                        aria-label="重新检测 TeX Live 工具"
                        title="重新检测 TeX Live 工具"
                        type="button"
                        onClick={() => void loadTexLiveSettings()}
                      >
                        <RefreshCw size={14} aria-hidden="true" />
                      </button>
                    </div>
                    {texLiveLoading ? (
                      <p className="source-editor-sidebar-empty">
                        正在检测 TeX 工具...
                      </p>
                    ) : texLiveEnvironment ? (
                      <div className="source-editor-texlive-tools">
                        {(
                          Object.keys(texLiveToolLabels) as Array<
                            keyof typeof texLiveToolLabels
                          >
                        ).map((name) => {
                          const tool = texLiveEnvironment.tools[name];
                          return (
                            <div key={name}>
                              {tool.available ? (
                                <CheckCircle2
                                  className="source-editor-tool-ok"
                                  size={14}
                                  aria-hidden="true"
                                />
                              ) : (
                                <CircleAlert
                                  className="source-editor-tool-missing"
                                  size={14}
                                  aria-hidden="true"
                                />
                              )}
                              <span>{texLiveToolLabels[name]}</span>
                              <small>
                                {tool.available
                                  ? (tool.version ?? "已找到")
                                  : "未找到"}
                              </small>
                            </div>
                          );
                        })}
                      </div>
                    ) : (
                      <p className="source-editor-sidebar-empty">
                        尚未检测。打开此选项卡后会自动检查编译环境。
                      </p>
                    )}
                  </div>
                </div>
              )}
            </div>
          ) : (
            <>
              <div className="source-editor-sidebar-heading">
                <span>资源管理器</span>
                <div>
                  <button
                    title="刷新文件列表"
                    type="button"
                    onClick={refreshFiles}
                  >
                    <RefreshCw size={15} aria-hidden="true" />
                  </button>
                  <button
                    title="折叠所有文件夹"
                    type="button"
                    onClick={() => setExpandedDirectories(new Set())}
                  >
                    <ChevronRight size={15} aria-hidden="true" />
                  </button>
                </div>
              </div>
              <div className="source-editor-explorer-root">
                <FolderOpen size={16} aria-hidden="true" />
                <span title={project?.name}>{project?.name ?? "论文项目"}</span>
              </div>
              {loading ? (
                <p className="source-editor-sidebar-empty">正在读取...</p>
              ) : visibleTree.length ? (
                <div
                  className="source-editor-tree"
                  role="tree"
                  aria-label="源文件"
                >
                  {visibleTree.map((node) => (
                    <FileTreeItem
                      depth={0}
                      dirty={dirty}
                      expandedDirectories={expandedDirectories}
                      gitStatuses={gitFileStatuses}
                      key={node.path}
                      node={node}
                      onSelectFile={(file) => void selectFile(file)}
                      onToggleDirectory={toggleDirectory}
                      selectedPath={selectedPath}
                    />
                  ))}
                </div>
              ) : (
                <p className="source-editor-sidebar-empty">
                  {filter ? "没有匹配的文件" : "没有可编辑的源文件"}
                </p>
              )}
            </>
          )}
        </aside>

        <section className="source-editor-main" aria-label="源码编辑器">
          <div className="source-editor-breadcrumbs">
            <span>{project?.name ?? "论文项目"}</span>
            {selectedPath && (
              <>
                <ChevronRight size={14} aria-hidden="true" />
                <span>{selectedPath}</span>
              </>
            )}
          </div>
          <div className="source-editor-tabs" role="tablist">
            {selectedPath ? (
              <div
                className="source-editor-tab active"
                role="tab"
                aria-selected="true"
              >
                <FileCode2 size={15} aria-hidden="true" />
                <span>{selectedPath.split("/").pop()}</span>
                {dirty && <span className="source-editor-tab-dirty">●</span>}
                <button
                  aria-label="关闭当前文件"
                  title="关闭当前文件"
                  type="button"
                  onClick={() => {
                    if (
                      dirtyRef.current &&
                      !window.confirm(
                        "当前文件有未保存修改，关闭会丢失这些修改。继续吗？",
                      )
                    )
                      return;
                    discardCurrentEdits();
                    selectedPathRef.current = "";
                    contentRef.current = "";
                    savedContentRef.current = "";
                    setSelectedPath("");
                    setLoadedContent("");
                    setDirtyValue(false);
                  }}
                >
                  <X size={14} aria-hidden="true" />
                </button>
              </div>
            ) : (
              <div className="source-editor-tab-empty">没有打开的文件</div>
            )}
          </div>
          <div className="source-editor-canvas">
            {selectedPath ? (
              <>
                <MonacoEditor
                  defaultValue={loadedContent}
                  height="100%"
                  key={selectedPath}
                  language={languageFor(selectedPath)}
                  onChange={onEditorChange}
                  onMount={handleEditorMount}
                  options={{
                    automaticLayout: true,
                    bracketPairColorization: { enabled: true },
                    fontSize: 13,
                    largeFileOptimizations: true,
                    minimap: { enabled: false },
                    padding: { top: 10 },
                    renderWhitespace: "selection",
                    scrollBeyondLastLine: false,
                    smoothScrolling: false,
                    tabSize: 2,
                    wordWrap: "off",
                  }}
                  theme="vs-dark"
                />
                {fileLoading && (
                  <div className="source-editor-editor-overlay">
                    <span>正在读取文件...</span>
                  </div>
                )}
              </>
            ) : fileLoading || loading ? (
              <div className="source-editor-loading">正在读取文件...</div>
            ) : (
              <div className="source-editor-empty">
                从资源管理器选择一个源文件
              </div>
            )}
          </div>
        </section>
      </div>

      <footer className="source-editor-statusbar">
        <div className="source-editor-status-left">
          <span>
            <GitBranch size={13} aria-hidden="true" />
            工作区
          </span>
          <span className={error ? "is-error" : ""}>
            {error ||
              status ||
              (saving ? "保存中..." : dirty ? "未保存" : "已保存")}
          </span>
        </div>
        <div className="source-editor-status-right">
          {selectedPath && (
            <span>
              Ln {cursor.line}, Col {cursor.column}
            </span>
          )}
          <span>Spaces: 2</span>
          <span>UTF-8</span>
          <span>{selectedPath ? languageFor(selectedPath) : "纯文本"}</span>
        </div>
      </footer>
    </main>
  );
}
