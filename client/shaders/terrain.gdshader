// Aetheria terrain: two-tone value-noise ground, one shader reused with
// different uniforms for the town pocket (hidden moss) vs the Emberfield
// (burnt soil). Shading is unlit-ish so a wide plane stays flat.
shader_type spatial;
render_mode unshaded;

uniform vec3 base : source_color = vec3(0.24, 0.19, 0.14);
uniform vec3 tint : source_color = vec3(0.36, 0.30, 0.12);
uniform float cells = 26.0;
uniform float blend = 0.35;

float hash(vec2 p) {
	return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453123);
}

float noise(vec2 p) {
	vec2 i = floor(p);
	vec2 f = fract(p);
	f = f * f * (3.0 - 2.0 * f);
	return mix(
		mix(hash(i), hash(i + vec2(1.0, 0.0)), f.x),
		mix(hash(i + vec2(0.0, 1.0)), hash(i + vec2(1.0, 1.0)), f.x),
		f.y);
}

float fbm(vec2 p) {
	return noise(p) * 0.6 + noise(p * 2.35) * 0.4;
}

void fragment() {
	float n = fbm(UV * cells);
	vec3 col = mix(base, tint, clamp((n - 0.5) * blend * 2.0 + 0.5, 0.0, 1.0));
	vec3 shore = base * 1.35;
	float ring = distance(UV, vec2(0.5)) * 2.0;
	if (ring < 0.02) {
		col = mix(col, shore, 0.85); // shrine ring
	}
	ALBEDO = col;
	ROUGHNESS = 0.98;
}