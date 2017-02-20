// @flow
// Automatically generated by typewriter. Do not edit.
// http://www.github.com/natdm/typewriter


//Data should all parse right.
//It's hard to get them to do that.
//@strict
export type Data = {| 
	map_string_to_int: override_map_name, //I am a map of strings and ints

	map_string_to_ints: { [key: string]: Array<number> }, //I am a map of strings to a slice of ints

	map_string_to_maps: ?override_map_name2, //I am a map of strings to maps

	peeps: People
|}

//Embedded will take all types from the embedded types and insert them in to the new type.
export type Embedded = { 
	name: string, 
	age: number
}

export type ExternalEmbedded = { 
	name: string
}

export type MyNumber = number

//Nested defaults to the closest "Object" type in any language. Utilize the `tw` tag if needed.
export type Nested = { 
	person: Object
}

//People is a map of strings to person
export type People = { [key: string]: Person }

//Person ...
export type Person = { 
	name: string, 
	age: number
}

export type Thing = { 
	name: number
}
