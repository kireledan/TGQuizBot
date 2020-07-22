package bot

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"time"

	"github.com/anaskhan96/soup"
)

type Quiz struct {
	Questions []Question
}

type Question struct {
	Question string
	Choices  []string
	Correct  []int
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func (q Quiz) GetRandomQuestion() Question {
	s := rand.NewSource(time.Now().Unix())
	r := rand.New(s) // initialize local pseudorandom generator

	randomQ := q.Questions[r.Intn(len(q.Questions))]
	if len(randomQ.Choices) <= 1 {
		randomQ = q.Questions[r.Intn(len(q.Questions))]
	}
	return randomQ
}

func GetQuiz() Quiz {
	quizFiles := []string{"./exam1.html", "./exam2.html", "./exam3.html", "./exam4.html", "./exam5.html"}
	AllQuestions := []Question{}

	for _, qfile := range quizFiles {
		dat, err := ioutil.ReadFile(qfile)
		check(err)

		doc := soup.HTMLParse(string(dat))
		Questions := doc.FindAll("div", "class", "panel-heading")
		FoundQuestions := make([]Question, len(Questions))

		for num, link := range Questions {
			titles := link.Find("div")
			if titles.Pointer != nil {
				FoundQuestions[num] = Question{Question: titles.Text()}
			}
		}

		Choices := doc.FindAll("ul", "class", "list-group")

		for num, answer := range Choices {
			choices := answer.FindAll("li", "class", "list-group-item")
			answers := []string{}
			correctAnswer := []int{}

			for choiceNum, choice := range choices {
				answers = append(answers, choice.Text())
				for _, children := range choice.Children() {
					if children.Attrs()["title"] == "Correct answer" {
						correctAnswer = append(correctAnswer, choiceNum)
					}
				}
			}
			FoundQuestions[num].Choices = answers
			FoundQuestions[num].Correct = correctAnswer
		}
		AllQuestions = append(AllQuestions, FoundQuestions...)
	}

	// for _, question := range AllQuestions {
	// 	fmt.Println(question.Question)
	// 	fmt.Println(question.Choices)
	// 	fmt.Println(len(question.Choices))
	// }
	fmt.Println("Loaded questions")
	fmt.Println(len(AllQuestions))

	return Quiz{AllQuestions}
}

func remove(slice []Question, s int) []Question {
	return append(slice[:s], slice[s+1:]...)
}
