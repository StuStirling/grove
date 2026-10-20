export namespace main {
	
	export class RepoInit {
	    name: string;
	    config: string;
	    worktreeRoot: string;
	    base: string;
	    branches: string[];
	
	    static createFrom(source: any = {}) {
	        return new RepoInit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.config = source["config"];
	        this.worktreeRoot = source["worktreeRoot"];
	        this.base = source["base"];
	        this.branches = source["branches"];
	    }
	}
	export class AddedRepo {
	    name: string;
	    init?: RepoInit;
	
	    static createFrom(source: any = {}) {
	        return new AddedRepo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.init = this.convertValues(source["init"], RepoInit);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BranchResult {
	    kept: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new BranchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kept = source["kept"];
	        this.detail = source["detail"];
	    }
	}
	export class Workspace {
	    name: string;
	    dir: string;
	    panes: string[];
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.dir = source["dir"];
	        this.panes = source["panes"];
	    }
	}
	export class Repo {
	    path: string;
	    prefix: string;
	    panes: string[];
	    worktreeRoot: string;
	    base: string;
	    setup: string;
	
	    static createFrom(source: any = {}) {
	        return new Repo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.prefix = source["prefix"];
	        this.panes = source["panes"];
	        this.worktreeRoot = source["worktreeRoot"];
	        this.base = source["base"];
	        this.setup = source["setup"];
	    }
	}
	export class Config {
	    terminal: string;
	    fontFamily: string;
	    fontSize: number;
	    repo: Repo[];
	    workspace: Workspace[];
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.terminal = source["terminal"];
	        this.fontFamily = source["fontFamily"];
	        this.fontSize = source["fontSize"];
	        this.repo = this.convertValues(source["repo"], Repo);
	        this.workspace = this.convertValues(source["workspace"], Workspace);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Pane {
	    id: string;
	    cmd: string;
	    kind: string;
	    name: string;
	    claude: string;
	
	    static createFrom(source: any = {}) {
	        return new Pane(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.cmd = source["cmd"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.claude = source["claude"];
	    }
	}
	export class RemoveResult {
	    status: string;
	    reason: string;
	    detail: string;
	    branchKept: string;
	    branchDetail: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.reason = source["reason"];
	        this.detail = source["detail"];
	        this.branchKept = source["branchKept"];
	        this.branchDetail = source["branchDetail"];
	    }
	}
	
	export class RepoInfo {
	    name: string;
	    path: string;
	    added: boolean;
	    config: string;
	
	    static createFrom(source: any = {}) {
	        return new RepoInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.added = source["added"];
	        this.config = source["config"];
	    }
	}
	
	export class SettingsFile {
	    config: Config;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new SettingsFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.config = this.convertValues(source["config"], Config);
	        this.version = source["version"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ShortcutInfo {
	    group: string;
	    label: string;
	    keys: string;
	
	    static createFrom(source: any = {}) {
	        return new ShortcutInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.group = source["group"];
	        this.label = source["label"];
	        this.keys = source["keys"];
	    }
	}
	export class WorkspaceInfo {
	    name: string;
	    branch: string;
	    dir: string;
	    repo: string;
	    repoPath: string;
	    config: string;
	    open: boolean;
	    claude: string;
	    panes: Pane[];
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.branch = source["branch"];
	        this.dir = source["dir"];
	        this.repo = source["repo"];
	        this.repoPath = source["repoPath"];
	        this.config = source["config"];
	        this.open = source["open"];
	        this.claude = source["claude"];
	        this.panes = this.convertValues(source["panes"], Pane);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Snapshot {
	    error: string;
	    config: string;
	    init?: RepoInit;
	    workspaces: WorkspaceInfo[];
	    repos: RepoInfo[];
	    canCreate: boolean;
	    terminal: boolean;
	    fontFamily: string;
	    fontSize: number;
	    shortcuts: ShortcutInfo[];
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.error = source["error"];
	        this.config = source["config"];
	        this.init = this.convertValues(source["init"], RepoInit);
	        this.workspaces = this.convertValues(source["workspaces"], WorkspaceInfo);
	        this.repos = this.convertValues(source["repos"], RepoInfo);
	        this.canCreate = source["canCreate"];
	        this.terminal = source["terminal"];
	        this.fontFamily = source["fontFamily"];
	        this.fontSize = source["fontSize"];
	        this.shortcuts = this.convertValues(source["shortcuts"], ShortcutInfo);
	        this.version = source["version"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	

}

