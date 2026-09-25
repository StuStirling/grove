export namespace main {
	
	export class Pane {
	    id: string;
	    cmd: string;
	
	    static createFrom(source: any = {}) {
	        return new Pane(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.cmd = source["cmd"];
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
	    workspaces: WorkspaceInfo[];
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
	        this.workspaces = this.convertValues(source["workspaces"], WorkspaceInfo);
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

