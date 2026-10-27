export namespace main {
	
	export class Endpoint {
	    port: number;
	    token: string;
	    version: string;
	    contract: number;
	    base_url: string;
	
	    static createFrom(source: any = {}) {
	        return new Endpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.token = source["token"];
	        this.version = source["version"];
	        this.contract = source["contract"];
	        this.base_url = source["base_url"];
	    }
	}

}

