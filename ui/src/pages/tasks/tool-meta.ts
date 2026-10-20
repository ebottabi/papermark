import {
  Calendar,
  FileText,
  Folder,
  Globe,
  Mail,
  Search,
  Terminal,
  Users,
  Wrench,
  type LucideIcon,
} from "lucide-react";

export type ToolMeta = {
  icon: LucideIcon;
  title: string;
  subject: string;
  mono?: boolean;
  accentHex?: string;
};

function asRecord(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
}

function pick(input: unknown, keys: string[]): string {
  const r = asRecord(input);
  for (const k of keys) {
    const v = r[k];
    if (typeof v === "string" && v.trim()) return v;
    if (typeof v === "number") return String(v);
  }
  return "";
}

/** Prettify a raw tool name: "web_search" / "web.search" -> "Web search". */
function prettify(name: string): string {
  const words = name.replace(/[_.]+/g, " ").trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : name;
}

// Connector glyphs carry their brand hue; everything else stays neutral ink.
const CONNECTOR: Record<string, { icon: LucideIcon; hex: string }> = {
  gmail: { icon: Mail, hex: "#EA4335" },
  email: { icon: Mail, hex: "#EA4335" },
  calendar: { icon: Calendar, hex: "#4285F4" },
  contacts: { icon: Users, hex: "#1A73E8" },
  drive: { icon: Folder, hex: "#0F9D58" },
};

export function toolMeta(name: string, input: unknown): ToolMeta {
  switch (name) {
    case "web_search":
      return { icon: Search, title: "Searching the web", subject: pick(input, ["query", "q", "text"]) };
    case "web_fetch":
      return { icon: Globe, title: "Reading a page", subject: pick(input, ["url", "href"]), mono: true };
    case "shell_exec":
      return { icon: Terminal, title: "Running a command", subject: pick(input, ["command", "cmd", "script"]), mono: true };
    case "file_read":
      return { icon: FileText, title: "Reading a file", subject: pick(input, ["path", "file_path", "file"]), mono: true };
    case "file_write":
      return { icon: FileText, title: "Writing a file", subject: pick(input, ["path", "file_path", "file"]), mono: true };
  }

  for (const key of Object.keys(CONNECTOR)) {
    if (name.toLowerCase().includes(key)) {
      const c = CONNECTOR[key];
      return {
        icon: c.icon,
        title: prettify(name),
        subject: pick(input, ["to", "subject", "query", "summary", "title", "name", "url", "path"]),
        accentHex: c.hex,
      };
    }
  }

  return {
    icon: Wrench,
    title: prettify(name),
    subject: pick(input, ["query", "url", "path", "command", "text", "subject", "name"]),
  };
}
