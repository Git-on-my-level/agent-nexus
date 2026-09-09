#include <stdio.h>
#include <string.h>

int main(void) {
	char input[1048576];
	size_t n = fread(input, 1, sizeof(input) - 1, stdin);
	input[n] = 0;
	if (strstr(input, "\"reject\":true")) {
		puts("{\"error\":\"rejected\"}");
		return 0;
	}
	puts("{\"facts\":{\"reader\":\"generated-c-transform\",\"has_snapshot\":true},\"uncertainty\":[\"generated transform; source status is reported, not independently verified\"],\"evidence\":[]}");
	return 0;
}
